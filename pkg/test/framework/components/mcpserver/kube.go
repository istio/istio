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
				readinessProbe:
					tcpSocket:
						port: grpc
					initialDelaySeconds: 1
`
)

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

func newKube(ctx resource.Context) (Instance, error) {
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
		"deployment":      "mcp-sinkserver",
		"app":             "mcp-sinkserver",
		"version":         "test",
		"port":            mcpserver.FakeServerPort,
	})
	if err != nil {
		return nil, err
	}

	c.deployment = deployment.NewYamlContentDeployment(c.namespace.Name(), yamlContent)
	if err = c.deployment.Deploy(env.Accessor, false); err != nil {
		scopes.CI.Info("Error applying MCPServer (Sink) deployment config")
		return nil, err
	}

	podFetchFunc := env.NewSinglePodFetch(c.namespace.Name(), "app=mcp-sinkserver", "version=test")
	pods, err := env.WaitUntilPodsAreReady(podFetchFunc)
	if err != nil {
		scopes.CI.Infof("Error waiting for MCPServer (Sink) pod to become running: %v", err)
		return nil, err
	}
	pod := pods[0]

	var svc *kubeApiCore.Service
	if svc, _, err = env.WaitUntilServiceEndpointsAreReady(c.namespace.Name(), "mcp-sinkserver"); err != nil {
		scopes.CI.Infof("Error waiting for MCPServer (Sink) service to be available: %v", err)
		return nil, err
	}

	c.svcAddress = fmt.Sprintf("%s:%d", svc.Spec.ClusterIP, svc.Spec.Ports[0].TargetPort.IntVal)
	scopes.Framework.Infof("MCPServer (Sink) in-cluster address: %s", c.svcAddress)

	if c.forwarder, err = env.NewPortForwarder(
		pod, 0, uint16(svc.Spec.Ports[0].TargetPort.IntValue())); err != nil {
		scopes.CI.Infof("Error setting up PortForwarder for MCP Server (Sink): %v", err)
		return nil, err
	}

	if err = c.forwarder.Start(); err != nil {
		scopes.CI.Infof("Error starting PortForwarder for MCP Server (Sink): %v", err)
		return nil, err
	}

	if c.client, err = newClient(c.svcAddress); err != nil {
		scopes.CI.Infof("Error while creating MCPServer (Sink) client: %v", err)
		return nil, err
	}
	return c, nil
}

func (c *kubeComponent) Address() string {
	return c.svcAddress
}

func (c *kubeComponent) GetCollectionStateOrFail(t test.Failer, collection string) []*sink.Object {
	t.Helper()
	sinkObjects, err := c.client.GetCollectionState(collection)
	if err != nil {
		t.Fatalf("MCPServer.GetCollectionStateOrFail: %v", err)
	}
	return sinkObjects
}

func (c *kubeComponent) ID() resource.ID {
	return c.id
}

func (c *kubeComponent) Close() (err error) {
	if c.forwarder != nil {
		err = c.forwarder.Close()
		c.forwarder = nil
	}

	return err
}
