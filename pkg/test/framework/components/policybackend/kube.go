//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package policybackend

import (
	"fmt"
	"io"
	"time"

	"github.com/hashicorp/go-multierror"
	kubeApiCore "k8s.io/api/core/v1"

	istioKube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/framework/image"
	"istio.io/istio/pkg/test/util/retry"

	"istio.io/istio/pkg/test/fakes/policy"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	testKube "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/tmpl"
)

const (
	template = `
# Test Policy Backend
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
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{.deployment}}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: {{.app}}
      version: {{.version}}
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
        image: "{{.Hub}}/test_policybackend:{{.Tag}}"
        imagePullPolicy: {{.ImagePullPolicy}}
        securityContext:
          runAsUser: 1
        ports:
        - name: grpc
          containerPort: {{.port}}
        readinessProbe:
          tcpSocket:
            port: grpc
          initialDelaySeconds: 1
---
`

	inProcessHandlerKube = `
apiVersion: "config.istio.io/v1alpha2"
kind: handler
metadata:
  name: %s
spec:
  params:
    backend_address: policy-backend.%s.svc.cluster.local:1071
  compiledAdapter: bypass
---
`

	outOfProcessHandlerKube = `
apiVersion: "config.istio.io/v1alpha2"
kind: handler
metadata:
  name: allowhandler
spec:
  adapter: policybackend
  connection:
    address: policy-backend.%s.svc.cluster.local:1071
  params:
    checkParams:
      checkAllow: true
      validDuration: 10s
      validCount: 1
---
apiVersion: "config.istio.io/v1alpha2"
kind: handler
metadata:
  name: denyhandler
spec:
  adapter: policybackend
  connection:
    address: policy-backend.%s.svc.cluster.local:1071
  params:
    checkParams:
      checkAllow: false
---
apiVersion: "config.istio.io/v1alpha2"
kind: handler
metadata:
  name: keyval
spec:
  adapter: policybackend
  connection:
    address: policy-backend.%s.svc.cluster.local:1071
  params:
    table:
      jason: admin
---
`
)

var (
	_ Instance        = &kubeComponent{}
	_ io.Closer       = &kubeComponent{}
	_ resource.Dumper = &kubeComponent{}
)

type kubeComponent struct {
	id resource.ID

	*client

	ctx       resource.Context
	namespace namespace.Instance

	forwarder   istioKube.PortForwarder
	appliedYAML string

	cluster resource.Cluster
}

// NewKubeComponent factory function for the component
func newKube(ctx resource.Context, cfg Config) (Instance, error) {
	c := &kubeComponent{
		ctx:     ctx,
		client:  &client{},
		cluster: ctx.Clusters().GetOrDefault(cfg.Cluster),
	}
	c.id = ctx.TrackResource(c)

	var err error
	scopes.Framework.Infof("=== BEGIN: PolicyBackend Deployment ===")
	defer func() {
		if err != nil {
			scopes.Framework.Infof("=== FAILED: PolicyBackend Deployment ===")
			_ = c.Close()
		} else {
			scopes.Framework.Infof("=== SUCCEEDED: PolicyBackend Deployment ===")
		}
	}()

	c.namespace, err = namespace.New(ctx, namespace.Config{
		Prefix: "policybackend",
	})
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
		"deployment":      "policy-backend",
		"app":             "policy-backend",
		"version":         "test",
		"port":            policy.DefaultPort,
	})
	if err != nil {
		return nil, err
	}

	if err = ctx.Config(c.cluster).ApplyYAML(c.namespace.Name(), yamlContent); err != nil {
		scopes.Framework.Info("Error applying PolicyBackend deployment config")
		return nil, err
	}
	c.appliedYAML = yamlContent

	podFetchFunc := testKube.NewSinglePodFetch(c.cluster, c.namespace.Name(), "app=policy-backend", "version=test")
	pods, err := testKube.WaitUntilPodsAreReady(podFetchFunc)
	if err != nil {
		scopes.Framework.Infof("Error waiting for PolicyBackend pod to become running: %v", err)
		return nil, err
	}
	pod := pods[0]

	var svc *kubeApiCore.Service
	if svc, _, err = testKube.WaitUntilServiceEndpointsAreReady(c.cluster, c.namespace.Name(), "policy-backend"); err != nil {
		scopes.Framework.Infof("Error waiting for PolicyBackend service to be available: %v", err)
		return nil, err
	}

	address := fmt.Sprintf("%s:%d", svc.Spec.ClusterIP, svc.Spec.Ports[0].TargetPort.IntVal)
	scopes.Framework.Infof("Policy Backend in-cluster address: %s", address)

	if c.forwarder, err = c.cluster.NewPortForwarder(
		pod.Name, pod.Namespace, "", 0, svc.Spec.Ports[0].TargetPort.IntValue()); err != nil {
		scopes.Framework.Infof("Error setting up PortForwarder for PolicyBackend: %v", err)
		return nil, err
	}

	if err = c.forwarder.Start(); err != nil {
		scopes.Framework.Infof("Error starting PortForwarder for PolicyBackend: %v", err)
		return nil, err
	}

	if c.client.controller, err = policy.NewController(c.forwarder.Address()); err != nil {
		scopes.Framework.Infof("Error starting Controller for PolicyBackend: %v", err)
		return nil, err
	}

	return c, nil
}

func (c *kubeComponent) CreateConfigSnippet(name string, _ string, am AdapterMode) string {
	switch am {
	case InProcess:
		return fmt.Sprintf(inProcessHandlerKube, name, c.namespace.Name())
	case OutOfProcess:
		handler := fmt.Sprintf(outOfProcessHandlerKube, c.namespace.Name(), c.namespace.Name(), c.namespace.Name())
		return handler
	default:
		scopes.Framework.Errorf("Error generating config snippet for policy backend: unsupported adapter mode")
		return ""
	}
}

func (c *kubeComponent) ID() resource.ID {
	return c.id
}

func (c *kubeComponent) Close() (err error) {
	if len(c.appliedYAML) > 0 {
		if err = c.ctx.Config(c.cluster).DeleteYAML(c.namespace.Name(), c.appliedYAML); err == nil {
			if e := testKube.WaitForNamespaceDeletion(c.cluster, c.namespace.Name(), retry.Timeout(time.Minute*5),
				retry.Delay(time.Second*5)); e != nil {
				scopes.Framework.Warnf("Error waiting for PolicyBackend deletion: %v", e)
				err = multierror.Append(err, e)
			}
		}
	}

	if c.forwarder != nil {
		c.forwarder.Close()
		c.forwarder = nil
	}

	return err
}

func (c *kubeComponent) Dump() {
	workDir, err := c.ctx.CreateTmpDirectory("policy-backend-state")
	if err != nil {
		scopes.Framework.Errorf("Unable to create dump folder for policy-backend-state: %v", err)
		return
	}
	testKube.DumpPods(c.cluster, workDir, c.namespace.Name(),
		testKube.DumpPodEvents,
		testKube.DumpPodLogs,
	)
}
