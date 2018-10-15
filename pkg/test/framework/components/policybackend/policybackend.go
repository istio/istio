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
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"

	"istio.io/istio/pkg/test/framework/internal"
	"istio.io/istio/pkg/test/framework/scopes"
	"istio.io/istio/pkg/test/util"

	"io"

	"istio.io/istio/pkg/test/fakes/policy"
	"istio.io/istio/pkg/test/framework/dependency"
	"istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/environments/kubernetes"
	"istio.io/istio/pkg/test/framework/environments/local"
	"istio.io/istio/pkg/test/framework/tmpl"
	"istio.io/istio/pkg/test/kube"
)

var (
	// LocalComponent is a component for the local environment.
	LocalComponent = &localComponent{}

	// KubeComponent is a component for the Kubernetes environment.
	KubeComponent = &kubeComponent{}

	_ environment.DeployedPolicyBackend = &policyBackend{}
	_ io.Closer                         = &policyBackend{}
	_ internal.Resettable               = &policyBackend{}
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

type localComponent struct {
}

// ID implements the component.Component interface.
func (c *localComponent) ID() dependency.Instance {
	return dependency.PolicyBackend
}

// Requires implements the component.Component interface.
func (c *localComponent) Requires() []dependency.Instance {
	return make([]dependency.Instance, 0)
}

// Init implements the component.Component interface.
func (c *localComponent) Init(ctx environment.ComponentContext, deps map[dependency.Instance]interface{}) (interface{}, error) {
	_, ok := ctx.Environment().(*local.Implementation)
	if !ok {
		return nil, fmt.Errorf("unsupported environment: %q", ctx.Environment().EnvironmentID())
	}

	backend := policy.NewPolicyBackend(0) // auto-allocate port
	err := backend.Start()
	if err != nil {
		return nil, err
	}

	port := backend.Port()

	controller, err := util.Retry(util.DefaultRetryWait, time.Second, func() (interface{}, bool, error) {
		c, err := policy.NewController(fmt.Sprintf(":%d", port))
		if err != nil {
			scopes.Framework.Debugf("error while connecting to the PolicyBackend controller: %v", err)
			return nil, false, err
		}
		return c, true, nil
	})

	if err != nil {
		_ = backend.Close()
		return nil, err
	}

	return &policyBackend{
		port:       port,
		backend:    backend,
		controller: controller.(*policy.Controller),
		env:        ctx.Environment(),
		local:      true,
	}, nil
}

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
func (c *kubeComponent) Init(ctx environment.ComponentContext, deps map[dependency.Instance]interface{}) (
	r interface{}, err error) {

	e, ok := ctx.Environment().(*kubernetes.Implementation)
	if !ok {
		return nil, fmt.Errorf("unsupported environment: %q", ctx.Environment().EnvironmentID())
	}

	scopes.CI.Infof("=== BEGIN: PolicyBackend Deployment ===")
	defer func() {
		if err != nil {
			scopes.CI.Infof("=== FAILED: PolicyBackend Deployment ===")
		} else {
			scopes.CI.Infof("=== SUCCEEDED: PolicyBackend Deployment ===")
		}
	}()

	s := e.KubeSettings()

	result, err := tmpl.Evaluate(template, map[string]interface{}{
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

	if err = kube.ApplyContents(s.KubeConfig, s.DependencyNamespace, result); err != nil {
		scopes.CI.Info("Error applying PolicyBackend deployment config")
		return
	}

	pod, err := e.Accessor.WaitForPodBySelectors(s.DependencyNamespace, "app=policy-backend", "version=test")
	if err != nil {
		scopes.CI.Infof("Error waiting for PolicyBackend pod: %v", err)
		return
	}

	if err = e.Accessor.WaitUntilPodIsRunning(s.DependencyNamespace, pod.Name); err != nil {
		scopes.CI.Infof("Error waiting for PolicyBackend pod to become running: %v", err)
		return
	}

	svc, err := e.Accessor.GetService(s.DependencyNamespace, "policy-backend")
	if err != nil {
		scopes.CI.Infof("Error waiting for PolicyBackend service to be available: %v", err)
		return
	}

	addressInCluster := fmt.Sprintf("%s:%d", svc.Spec.ClusterIP, svc.Spec.Ports[0].TargetPort.IntVal)
	scopes.Framework.Infof("Policy Backend in-cluster address: %s", addressInCluster)

	options := &kube.PodSelectOptions{
		PodNamespace: pod.Namespace,
		PodName:      pod.Name,
	}

	var forwarder kube.PortForwarder
	if forwarder, err = kube.NewPortForwarder(
		s.KubeConfig, options, 0, uint16(svc.Spec.Ports[0].TargetPort.IntValue())); err != nil {
		scopes.CI.Infof("Error setting up PortForwarder for PolicyBackend: %v", err)
		return
	}

	if err = forwarder.Start(); err != nil {
		scopes.CI.Infof("Error starting PortForwarder for PolicyBackend: %v", err)
		return
	}

	var controller *policy.Controller
	if controller, err = policy.NewController(forwarder.Address()); err != nil {
		scopes.CI.Infof("Error starting Controller for PolicyBackend: %v", err)
		forwarder.Close()
		return
	}

	r = &policyBackend{
		address:             addressInCluster,
		dependencyNamespace: s.DependencyNamespace,
		controller:          controller,
		forwarder:           forwarder,
		env:                 ctx.Environment(),
		local:               false,
	}

	return
}

type policyBackend struct {
	address             string
	dependencyNamespace string
	controller          *policy.Controller
	forwarder           kube.PortForwarder
	env                 environment.Implementation

	// local only settings
	port    int
	backend *policy.Backend

	local bool
}

// Reset implements internal.Resettable.
func (p *policyBackend) Reset() error {
	return p.controller.Reset()
}

// DenyCheck implementation
func (p *policyBackend) DenyCheck(t testing.TB, deny bool) {
	t.Helper()

	if err := p.controller.DenyCheck(deny); err != nil {
		t.Fatalf("Error setting DenyCheck: %v", err)
	}
}

// ExpectReport implementation
func (p *policyBackend) ExpectReport(t testing.TB, expected ...proto.Message) {
	t.Helper()

	_, err := util.Retry(util.DefaultRetryTimeout, util.DefaultRetryWait, func() (interface{}, bool, error) {
		reports, err := p.controller.GetReports()
		if err != nil {
			return nil, false, err
		}

		if !contains(protoArrayToInterfaceArray(reports), protoArrayToInterfaceArray(expected)) {
			return nil, false, fmt.Errorf("expected reports not found.\nExpected:\n%v\nActual:\n%v",
				spew.Sdump(expected), spew.Sdump(reports))
		}

		return nil, true, nil
	})

	if err != nil {
		t.Fatalf("ExpectReport failed: %v", err)
	}
}

// ExpectReportJSON checks that the backend has received the given report request.
func (p *policyBackend) ExpectReportJSON(t testing.TB, expected ...string) {
	t.Helper()

	var err error
	for i, e := range expected {
		expected[i], err = p.env.Evaluate(e)
		if err != nil {
			t.Fatalf("template evaluation failed: %v", err)
		}
	}

	_, err = util.Retry(util.DefaultRetryTimeout, util.DefaultRetryWait, func() (interface{}, bool, error) {
		reports, err := p.controller.GetReports()
		if err != nil {
			return nil, false, err
		}

		m := jsonpb.Marshaler{
			Indent: "  ",
		}
		var actual []string
		for _, r := range reports {
			as, err := m.MarshalToString(r)
			if err != nil {
				t.Fatalf("Failed marshalling to string: %v", err)
			}
			actual = append(actual, as)
		}

		exMaps := jsonStringsToMaps(t, expected)
		acMaps := jsonStringsToMaps(t, actual)

		if !contains(mapArrayToInterfaceArray(acMaps), mapArrayToInterfaceArray(exMaps)) {
			return nil, false, fmt.Errorf("expected reports not found.\nExpected:\n%v\nActual:\n%v", expected, actual)
		}

		return nil, true, nil
	})

	if err != nil {
		t.Fatalf("ExpectReportJSON failed: %v", err)
	}
}

// contains checks whether items contains all entries in expected.
func contains(items, expected []interface{}) bool {

mainloop:
	for _, e := range expected {
		for _, i := range items {
			if reflect.DeepEqual(e, i) {
				continue mainloop
			}
		}
		return false
	}

	return true
}

func protoArrayToInterfaceArray(arr []proto.Message) []interface{} {
	result := make([]interface{}, len(arr))
	for i, p := range arr {
		result[i] = p
	}
	return result
}

func mapArrayToInterfaceArray(arr []map[string]interface{}) []interface{} {
	result := make([]interface{}, len(arr))
	for i, p := range arr {
		result[i] = p
	}
	return result
}

func jsonStringsToMaps(t testing.TB, arr []string) []map[string]interface{} {
	var result []map[string]interface{}

	for _, a := range arr {
		i := make(map[string]interface{})
		if err := json.Unmarshal([]byte(a), &i); err != nil {
			t.Fatalf("Error unmarshaling JSON: %v", err)
		}
		result = append(result, i)
	}

	return result
}

// CreateConfigSnippetImplementation
func (p *policyBackend) CreateConfigSnippet(name string) string {
	if p.local {
		return fmt.Sprintf(
			`apiVersion: "config.istio.io/v1alpha2"
kind: bypass
metadata:
  name: %s
  namespace: {{.TestNamespace}}
spec:
  backend_address: 127.0.0.1:%d
`, name, p.port)
	}

	return fmt.Sprintf(
		`apiVersion: "config.istio.io/v1alpha2"
kind: bypass
metadata:
  name: %s
spec:
  backend_address: policy-backend.%s.svc.cluster.local:1071
`, name, p.dependencyNamespace)
}

// Close implementation.
func (p *policyBackend) Close() error {
	if p.forwarder != nil {
		p.forwarder.Close()
	}

	return nil
}
