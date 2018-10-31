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

	v12 "k8s.io/api/core/v1"

	"istio.io/istio/pkg/test/fakes/policy"
	"istio.io/istio/pkg/test/framework/dependency"
	"istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/environments/kubernetes"
	"istio.io/istio/pkg/test/framework/scopes"
	"istio.io/istio/pkg/test/framework/tmpl"
	"istio.io/istio/pkg/test/kube"
)

var (
	// KubeComponent is a component for the Kubernetes environment.
	KubeComponent = &kubeComponent{}
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

type kubeComponent struct {
}

// ID implements the component.Component interface.
func (c *kubeComponent) ID() dependency.Instance {
	return dependency.PolicyBackend
}

// Requires implements the component.Component interface.
func (c *kubeComponent) Requires() []dependency.Instance {
	return make([]dependency.Instance, 0)
}

// Init implements the component.Component interface.
func (c *kubeComponent) Init(ctx environment.ComponentContext, deps map[dependency.Instance]interface{}) (r interface{}, err error) {
	e, ok := ctx.Environment().(*kubernetes.Implementation)
	if !ok {
		return nil, fmt.Errorf("unsupported environment: %q", ctx.Environment().EnvironmentID())
	}

	s := e.KubeSettings()
	be := &policyBackend{
		dependencyNamespace: s.DependencyNamespace,
		env:                 ctx.Environment(),
		local:               false,
	}
	scopes.CI.Infof("=== BEGIN: PolicyBackend Deployment ===")
	defer func() {
		if err != nil {
			scopes.CI.Infof("=== FAILED: PolicyBackend Deployment ===")
			_ = be.Close()
		} else {
			scopes.CI.Infof("=== SUCCEEDED: PolicyBackend Deployment ===")
		}
	}()

	yamlContent, err := tmpl.Evaluate(template, map[string]interface{}{
		"Hub":             s.Values[kubernetes.HubValuesKey],
		"Tag":             s.Values[kubernetes.TagValuesKey],
		"ImagePullPolicy": s.Values[kubernetes.ImagePullPolicyValuesKey],
		"deployment":      "policy-backend",
		"app":             "policy-backend",
		"version":         "test",
		"port":            policy.DefaultPort,
	})
	if err != nil {
		return
	}

	be.prependCloser(func() error {
		return e.Accessor.DeleteContents(s.DependencyNamespace, yamlContent)
	})
	if err = e.Accessor.ApplyContents(s.DependencyNamespace, yamlContent); err != nil {
		scopes.CI.Info("Error applying PolicyBackend deployment config")
		return
	}

	var pod v12.Pod
	pod, err = e.Accessor.WaitForPodBySelectors(s.DependencyNamespace, "app=policy-backend", "version=test")
	if err != nil {
		scopes.CI.Infof("Error waiting for PolicyBackend pod: %v", err)
		return
	}

	if err = e.Accessor.WaitUntilPodIsRunning(s.DependencyNamespace, pod.Name); err != nil {
		scopes.CI.Infof("Error waiting for PolicyBackend pod to become running: %v", err)
		return
	}

	var svc *v12.Service
	svc, err = e.Accessor.GetService(s.DependencyNamespace, "policy-backend")
	if err != nil {
		scopes.CI.Infof("Error waiting for PolicyBackend service to be available: %v", err)
		return
	}

	be.address = fmt.Sprintf("%s:%d", svc.Spec.ClusterIP, svc.Spec.Ports[0].TargetPort.IntVal)
	scopes.Framework.Infof("Policy Backend in-cluster address: %s", be.address)

	options := &kube.PodSelectOptions{
		PodNamespace: pod.Namespace,
		PodName:      pod.Name,
	}

	var forwarder kube.PortForwarder
	if forwarder, err = e.Accessor.NewPortForwarder(
		options, 0, uint16(svc.Spec.Ports[0].TargetPort.IntValue())); err != nil {
		scopes.CI.Infof("Error setting up PortForwarder for PolicyBackend: %v", err)
		return
	}

	if err = forwarder.Start(); err != nil {
		scopes.CI.Infof("Error starting PortForwarder for PolicyBackend: %v", err)
		return
	}
	be.prependCloser(forwarder.Close)

	if be.controller, err = policy.NewController(forwarder.Address()); err != nil {
		scopes.CI.Infof("Error starting Controller for PolicyBackend: %v", err)
		return
	}

	return be, nil
}
