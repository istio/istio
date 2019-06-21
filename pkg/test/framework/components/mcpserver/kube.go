package mcpserver

import (
	"fmt"

	"istio.io/istio/pkg/mcp/sink"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/deployment"
	"istio.io/istio/pkg/test/fakes/mcpserver"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/core/image"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/tmpl"

	kubeApiCore "k8s.io/api/core/v1"

	testKube "istio.io/istio/pkg/test/kube"
)

const (
	template = `
# MCP Server (Sink mode) template
apiVersion: v1
kind: Service
metadata:
  name: {{.app}}
  labels:
    app: {{.app}}
spec:
  ports:
  - port: {{.port}}
    targetPort: {{.port}}
    name: grpc
  selector:
    app: {{.app}}
---
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: {{.deployment}}
spec:
  replicas: 1
  template:
    metadata:
      labels:
        app: {{.app}}
        version: {{.version}}
      annotations:
        sidecar.istio.io/inject: "false"
    spec:
      containers:
      - name: app
        image: "{{.Hub}}/test_mcpserver:{{.Tag}}"
        imagePullPolicy: {{.ImagePullPolicy}}
        ports:
        - name: grpc
          containerPort: {{.port}}
`

	appName = "mcp-sinkserver"
)

// kubeComponent represents the MCP server component
// running in Kubernetes environment
type kubeComponent struct {
	id resource.ID

	*client
	ctx        resource.Context
	kubeEnv    *kube.Environment
	namespace  namespace.Instance
	forwarder  testKube.PortForwarder
	deployment *deployment.Instance
	svcAddress string
}

// NewKube sets up MCP server component in Kubernetes environment.
// It returns the handle to the component if successful, else an error
func newKube(ctx resource.Context, cfg SinkConfig) (Instance, error) {
	env := ctx.Environment().(*kube.Environment)
	c := &kubeComponent{
		ctx:     ctx,
		kubeEnv: env,
		client:  &client{},
	}
	c.id = ctx.TrackResource(c)

	var err error
	scopes.CI.Info("=== BEGIN: MCPServer Deployment in Sink mode ===")
	defer func() {
		if err != nil {
			scopes.CI.Infof("=== FAILED: MCPServer Deployment in Sink mode ===")
			_ = c.Close()
		} else {
			scopes.CI.Infof("=== SUCCEEEDED: MCPServer Deployment in Sink mode ===")
		}
	}()

	c.namespace, err = namespace.New(ctx, "mcpsinkserver", false)
	if err != nil {
		return nil, err
	}

	s, err := image.SettingsFromCommandLine()
	if err != nil {
		return nil, err
	}

	yamlContent, err := tmpl.Evaluate(template, map[string]interface{}{
		"Hub":             s.Hub,
		"Tag":             s.Tag,
		"ImagePullPolicy": s.PullPolicy,
		"deployment":      appName,
		"app":             appName,
		"version":         "test",
		"port":            mcpserver.FakeServerPort,
	})
	if err != nil {
		return nil, err
	}

	c.deployment = deployment.NewYamlContentDeployment(c.namespace.Name(), yamlContent)
	if err = c.deployment.Deploy(env.Accessor, true); err != nil {
		scopes.CI.Info("Error applying MCPServer (Sink) deployment config")
		return nil, err
	}

	podFetchFunc := env.NewSinglePodFetch(c.namespace.Name(), fmt.Sprintf("app=%s", appName), "version=test")
	pods, err := env.WaitUntilPodsAreReady(podFetchFunc)
	if err != nil {
		scopes.CI.Infof("Error waiting for MCPServer (Sink) pod to become running: %v", err)
		return nil, err
	}
	pod := pods[0]

	// Fetch MCP sink server to get the port in which it is listening
	var svc *kubeApiCore.Service
	if svc, _, err = env.WaitUntilServiceEndpointsAreReady(c.namespace.Name(), appName); err != nil {
		scopes.CI.Infof("Error waiting for MCPServer (Sink) service to be available: %v", err)
		return nil, err
	}

	remotePort := uint16(svc.Spec.Ports[0].TargetPort.IntValue())
	if c.forwarder, err = env.NewPortForwarder(pod, 0, remotePort); err != nil {
		scopes.CI.Infof("Error setting up PortForwarder for MCP Server (Sink): %v", err)
		return nil, err
	}

	// Start the forwarder. Now the component will be accessible from the test to make
	// calls to retrieve collections.
	if err = c.forwarder.Start(); err != nil {
		scopes.CI.Infof("Error starting PortForwarder for MCP Server (Sink): %v", err)
		return nil, err
	}
	scopes.Framework.Infof("forwading from %s -> %d", c.forwarder.Address(), remotePort)

	// svcAddress is the address needed for Galley to contact MCP sink. Since both Galley and MCP Sink
	// live inside Kubernetes cluster, Galley can find the MCP Sink from the service address (not the pod address)
	c.svcAddress = fmt.Sprintf("%s.%s.svc.cluster.local:6666", appName, c.namespace.Name())
	if c.client, err = newClient(c.forwarder.Address()); err != nil {
		scopes.CI.Infof("Error while creating MCPServer (Sink) client: %v", err)
		return nil, err
	}
	return c, nil
}

// Address returns the address of the MCP sink server endpoint. In case of native, it returns the actual
// host and port address. In Kubernetes mode, it returns service URL which can be DNS resolved in the form
// mcp-sinkserver.<namespace>.svc.cluster.local:port. This is required by Galley which also runs inside
// Kubernetes cluster.
func (c *kubeComponent) Address() string {
	return c.svcAddress
}

// GetCollectionStateOrFail fetches the given collection. This is purely for testing purposes and in both
// native and kubernetes environments it is called from the test.
func (c *kubeComponent) GetCollectionStateOrFail(t test.Failer, collection string) []*sink.Object {
	t.Helper()
	resources, err := c.client.GetCollectionState(collection)
	if err != nil {
		t.Fatalf("MCPServer.GetCollectionStateOrFail: %v", err)
	}
	return resources
}

// ID returns the ID of this resource
func (c *kubeComponent) ID() resource.ID {
	return c.id
}

// Close closes the forwarder connection.
func (c *kubeComponent) Close() (err error) {
	if c.forwarder != nil {
		err = c.forwarder.Close()
		c.forwarder = nil
	}
	return err
}
