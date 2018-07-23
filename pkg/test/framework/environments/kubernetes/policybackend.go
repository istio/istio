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

package kubernetes

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/protobuf/jsonpb"

	"istio.io/istio/pkg/test/fakes/policy"
	"istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/tmpl"
	"istio.io/istio/pkg/test/kube"
)

const template = `
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
        image: {{.Hub}}/test_policybackend:{{.Tag}}
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

type policyBackend struct {
	address             string
	dependencyNamespace string
	controller          *policy.Controller
	forwarder           *kube.PortForwarder
}

var _ environment.DeployedPolicyBackend = &policyBackend{}
var _ io.Closer = &policyBackend{}

func newPolicyBackend(e *Environment) (*policyBackend, error) {
	result, err := tmpl.Evaluate(template, map[string]interface{}{
		"Hub":             e.ctx.Hub(),
		"Tag":             e.ctx.Tag(),
		"deployment":      "policy-backend",
		"ImagePullPolicy": "Always",
		"app":             "policy-backend",
		"version":         "test",
		"port":            policy.DefaultPort,
	})

	if err != nil {
		return nil, err
	}

	if err = kube.ApplyContents(e.ctx.KubeConfigPath(), e.DependencyNamespace, result); err != nil {
		return nil, err
	}

	pod, err := e.accessor.WaitForPodBySelectors(e.DependencyNamespace, "app=policy-backend", "version=test")
	if err != nil {
		return nil, err
	}

	if err = e.accessor.WaitUntilPodIsRunning(e.DependencyNamespace, pod.Name); err != nil {
		return nil, err
	}

	if err = e.accessor.WaitUntilPodIsReady(e.DependencyNamespace, pod.Name); err != nil {
		return nil, err
	}

	svc, err := e.accessor.GetService(e.DependencyNamespace, "policy-backend")
	if err != nil {
		return nil, err
	}
	addressInCluster := fmt.Sprintf("%s:%d", svc.Spec.ClusterIP, svc.Spec.Ports[0].TargetPort.IntVal)
	scope.Debugf("Policy Backend in-cluster address: %s", addressInCluster)

	forwarder := kube.NewPortForwarder(e.ctx.KubeConfigPath(), pod.Namespace, pod.Name, int(svc.Spec.Ports[0].TargetPort.IntVal))
	if err = forwarder.Start(); err != nil {
		return nil, err
	}

	controller, err := policy.NewController(forwarder.Address())
	if err != nil {
		forwarder.Close()
		return nil, err
	}

	return &policyBackend{
		address:             addressInCluster,
		dependencyNamespace: e.DependencyNamespace,
		controller:          controller,
		forwarder:           forwarder,
	}, nil
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

	actual := p.accumulateReports(t, len(expected))

	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("Mismatch:\nActual:\n%v\nExpected:\n%v\n", spew.Sdump(actual), spew.Sdump(expected))
	}
}

// ExpectReportJSON checks that the backend has received the given report request.
func (p *policyBackend) ExpectReportJSON(t testing.TB, expected ...string) {
	t.Helper()

	acts := p.accumulateReports(t, len(expected))

	var actual []string
	for _, a := range acts {
		m := jsonpb.Marshaler{}
		as, err := m.MarshalToString(a)
		if err != nil {
			t.Fatalf("Failed marshalling to string: %v", err)
		}
		actual = append(actual, as)
	}

	exMaps := jsonStringsToMaps(t, expected)
	acMaps := jsonStringsToMaps(t, actual)

	if !reflect.DeepEqual(exMaps, acMaps) {
		t.Fatalf("Mismatch:\nActual:\n%v\nExpected:\n%v\n", actual, expected)
	}
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

const waitTime = time.Second * 15
const sleepDuration = time.Millisecond * 10

func (p *policyBackend) accumulateReports(t testing.TB, count int) []proto.Message {
	start := time.Now()

	actual := make([]proto.Message, 0, count)

	for len(actual) < count && start.Add(waitTime).After(time.Now()) {
		r, err := p.controller.GetReports()
		if err != nil {
			t.Fatalf("Error getting reports from policy backend: %v", err)
		}
		actual = append(actual, r...)
		if len(r) == 0 {
			time.Sleep(sleepDuration)
		}
	}

	if len(actual) < count {
		t.Fatalf("Unable accumulate enough protos before timeout: wanted:%d, accumulated:%d", count, len(actual))
	}

	return actual
}

// TODO: Fix hardwired code.

// CreateConfigSnippetImplementation
func (p *policyBackend) CreateConfigSnippet(name string) string {
	return fmt.Sprintf(
		`apiVersion: "config.istio.io/v1alpha2"
kind: bypass
metadata:
  name: %s
spec:
  backend_address: policy-backend.%s.svc:1071
`, name, p.dependencyNamespace)
}

// Close implementation.
func (p *policyBackend) Close() error {
	if p.forwarder != nil {
		p.forwarder.Close()
	}

	return nil
}
