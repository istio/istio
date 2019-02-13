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

	multierror "github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/deployment"
	"istio.io/istio/pkg/test/fakes/policy"
	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/components"
	"istio.io/istio/pkg/test/framework/api/context"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/pkg/test/framework/runtime/api"
	"istio.io/istio/pkg/test/framework/runtime/components/environment/kube"
	testKube "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"

	kubeApiCore "k8s.io/api/core/v1"
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
	_ components.PolicyBackend = &kubeComponent{}
	_ api.Component            = &kubeComponent{}
	_ api.Resettable           = &kubeComponent{}
	_ io.Closer                = &kubeComponent{}
)

// NewKubeComponent factory function for the component
func NewKubeComponent() (api.Component, error) {
	return &kubeComponent{}, nil
}

type kubeComponent struct {
	*client

	kubeEnv    *kube.Environment
	scope      lifecycle.Scope
	namespace  string
	forwarder  testKube.PortForwarder
	deployment *deployment.Instance
}

func (c *kubeComponent) Descriptor() component.Descriptor {
	return descriptors.PolicyBackend
}

func (c *kubeComponent) Scope() lifecycle.Scope {
	return c.scope
}

func (c *kubeComponent) Start(ctx context.Instance, scope lifecycle.Scope) (err error) {
	c.scope = scope

	env, err := kube.GetEnvironment(ctx)
	if err != nil {
		return err
	}
	c.kubeEnv = env

	c.namespace = env.NamespaceForScope(scope)
	c.client = &client{
		env: env,
	}
	scopes.CI.Infof("=== BEGIN: PolicyBackend Deployment ===")
	defer func() {
		if err != nil {
			scopes.CI.Infof("=== FAILED: PolicyBackend Deployment ===")
			_ = c.Close()
		} else {
			scopes.CI.Infof("=== SUCCEEDED: PolicyBackend Deployment ===")
		}
	}()

	values := env.HelmValueMap()
	yamlContent, err := env.EvaluateWithParams(template, map[string]interface{}{
		"Hub":             values[kube.HubValuesKey],
		"Tag":             values[kube.TagValuesKey],
		"ImagePullPolicy": values[kube.ImagePullPolicyValuesKey],
		"deployment":      "policy-backend",
		"app":             "policy-backend",
		"version":         "test",
		"port":            policy.DefaultPort,
	})
	if err != nil {
		return
	}

	c.deployment = deployment.NewYamlContentDeployment(c.namespace, yamlContent)
	if err = c.deployment.Deploy(env.Accessor, false); err != nil {
		scopes.CI.Info("Error applying PolicyBackend deployment config")
		return
	}

	podFetchFunc := env.NewSinglePodFetch(c.namespace, "app=policy-backend", "version=test")
	if err = env.WaitUntilPodsAreReady(podFetchFunc); err != nil {
		scopes.CI.Infof("Error waiting for PolicyBackend pod to become running: %v", err)
		return
	}
	var pods []kubeApiCore.Pod
	pods, err = podFetchFunc()
	if err != nil {
		return
	}
	pod := pods[0]

	var svc *kubeApiCore.Service
	svc, err = env.GetService(c.namespace, "policy-backend")
	if err != nil {
		scopes.CI.Infof("Error waiting for PolicyBackend service to be available: %v", err)
		return
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
		return
	}

	if err = c.forwarder.Start(); err != nil {
		scopes.CI.Infof("Error starting PortForwarder for PolicyBackend: %v", err)
		return
	}

	if c.client.controller, err = policy.NewController(c.forwarder.Address()); err != nil {
		scopes.CI.Infof("Error starting Controller for PolicyBackend: %v", err)
		return
	}

	return nil
}

func (c *kubeComponent) CreateConfigSnippet(name string) string {
	return fmt.Sprintf(
		`apiVersion: "config.istio.io/v1alpha2"
kind: bypass
metadata:
  name: %s
spec:
  backend_address: policy-backend.%s.svc.cluster.local:1071
`, name, c.namespace)
}

func (c *kubeComponent) Reset() error {
	if c.client != nil {
		return c.client.Reset()
	}
	return nil
}

func (c *kubeComponent) Close() (err error) {
	if c.forwarder != nil {
		err = multierror.Append(err, c.forwarder.Close()).ErrorOrNil()
	}

	// Don't delete the deployment if using Test scope, since the test namespace will be deleted later.
	if c.deployment != nil && c.scope != lifecycle.Test {
		err = multierror.Append(err, c.deployment.Delete(c.kubeEnv.Accessor, false)).ErrorOrNil()
		podFetchFunc := c.kubeEnv.NewPodFetch(c.namespace, "app=policy-backend", "version=test")
		err = multierror.Append(err, c.kubeEnv.WaitUntilPodsAreDeleted(podFetchFunc)).ErrorOrNil()
	}
	return
}
