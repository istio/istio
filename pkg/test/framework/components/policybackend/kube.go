//  Copyright 2018 Istio Authors
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
	"io/ioutil"
	"os"
	"path"

	kubeApiCore "k8s.io/api/core/v1"

	"istio.io/istio/pkg/test/deployment"
	"istio.io/istio/pkg/test/fakes/policy"
	deployment2 "istio.io/istio/pkg/test/framework/components/deployment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
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
        image: "{{.Hub}}/test_policybackend:{{.Tag}}"
        imagePullPolicy: {{.ImagePullPolicy}}
        ports:
        - name: grpc
          containerPort: {{.port}}
        readinessProbe:
          tcpSocket:
            port: grpc
          initialDelaySeconds: 1
---
`
)

var (
	_ Instance          = &kubeComponent{}
	_ resource.Resetter = &kubeComponent{}
	_ io.Closer         = &kubeComponent{}
	_ resource.Dumper   = &kubeComponent{}
)

type kubeComponent struct {
	id resource.ID

	*client

	ctx       resource.Context
	kubeEnv   *kube.Environment
	namespace namespace.Instance

	forwarder  testKube.PortForwarder
	deployment *deployment.Instance
}

// NewKubeComponent factory function for the component
func newKube(ctx resource.Context) (Instance, error) {
	env := ctx.Environment().(*kube.Environment)
	c := &kubeComponent{
		ctx:     ctx,
		kubeEnv: env,
		client:  &client{},
	}
	c.id = ctx.TrackResource(c)

	var err error
	scopes.CI.Infof("=== BEGIN: PolicyBackend Deployment ===")
	defer func() {
		if err != nil {
			scopes.CI.Infof("=== FAILED: PolicyBackend Deployment ===")
			_ = c.Close()
		} else {
			scopes.CI.Infof("=== SUCCEEDED: PolicyBackend Deployment ===")
		}
	}()

	c.namespace, err = namespace.New(ctx, "policybackend", false)
	if err != nil {
		return nil, err
	}

	s, err := deployment2.SettingsFromCommandLine()
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

	c.deployment = deployment.NewYamlContentDeployment(c.namespace.Name(), yamlContent)
	if err = c.deployment.Deploy(env.Accessor, false); err != nil {
		scopes.CI.Info("Error applying PolicyBackend deployment config")
		return nil, err
	}

	podFetchFunc := env.NewSinglePodFetch(c.namespace.Name(), "app=policy-backend", "version=test")
	if err = env.WaitUntilPodsAreReady(podFetchFunc); err != nil {
		scopes.CI.Infof("Error waiting for PolicyBackend pod to become running: %v", err)
		return nil, err
	}
	var pods []kubeApiCore.Pod
	pods, err = podFetchFunc()
	if err != nil {
		return nil, err
	}
	pod := pods[0]

	var svc *kubeApiCore.Service
	if svc, err = env.WaitUntilServiceEndpointsAreReady(c.namespace.Name(), "policy-backend"); err != nil {
		scopes.CI.Infof("Error waiting for PolicyBackend service to be available: %v", err)
		return nil, err
	}

	address := fmt.Sprintf("%s:%d", svc.Spec.ClusterIP, svc.Spec.Ports[0].TargetPort.IntVal)
	scopes.Framework.Infof("Policy Backend in-cluster address: %s", address)

	options := &testKube.PodSelectOptions{
		PodNamespace: pod.Namespace,
		PodName:      pod.Name,
	}

	if c.forwarder, err = env.NewPortForwarder(
		options, 0, uint16(svc.Spec.Ports[0].TargetPort.IntValue())); err != nil {
		scopes.CI.Infof("Error setting up PortForwarder for PolicyBackend: %v", err)
		return nil, err
	}

	if err = c.forwarder.Start(); err != nil {
		scopes.CI.Infof("Error starting PortForwarder for PolicyBackend: %v", err)
		return nil, err
	}

	if c.client.controller, err = policy.NewController(c.forwarder.Address()); err != nil {
		scopes.CI.Infof("Error starting Controller for PolicyBackend: %v", err)
		return nil, err
	}

	return c, nil
}

func (c *kubeComponent) CreateConfigSnippet(name string, namespace string) string {
	return fmt.Sprintf(
		`apiVersion: "config.istio.io/v1alpha2"
kind: handler
metadata:
  name: %s
spec:
  params:
    backend_address: policy-backend.%s.svc.cluster.local:1071
  compiledAdapter: bypass
`, name, c.namespace.Name())
}

func (c *kubeComponent) ID() resource.ID {
	return c.id
}

func (c *kubeComponent) Reset() error {
	if c.client != nil {
		return c.client.Reset()
	}
	return nil
}

func (c *kubeComponent) Close() (err error) {
	if c.forwarder != nil {
		err = c.forwarder.Close()
		c.forwarder = nil
	}

	return err
}

func (c *kubeComponent) Dump() {
	workDir, err := c.ctx.CreateTmpDirectory("policy-backend-state")
	if err != nil {
		scopes.CI.Errorf("Unable to create dump folder for policy-backend-state: %v", err)
		return
	}
	deployment.DumpPodState(workDir, c.namespace.Name(), c.kubeEnv.Accessor)

	pods, err := c.kubeEnv.Accessor.GetPods(c.namespace.Name())
	if err != nil {
		scopes.CI.Errorf("Unable to get pods from the system namespace: %v", err)
		return
	}

	for _, pod := range pods {
		for _, container := range pod.Spec.Containers {
			l, err := c.kubeEnv.Logs(pod.Namespace, pod.Name, container.Name)
			if err != nil {
				scopes.CI.Errorf("Unable to get logs for pod/container: %s/%s/%s", pod.Namespace, pod.Name, container.Name)
				continue
			}

			fname := path.Join(workDir, fmt.Sprintf("%s-%s.log", pod.Name, container.Name))
			if err = ioutil.WriteFile(fname, []byte(l), os.ModePerm); err != nil {
				scopes.CI.Errorf("Unable to write logs for pod/container: %s/%s/%s", pod.Namespace, pod.Name, container.Name)
			}
		}
	}
}
