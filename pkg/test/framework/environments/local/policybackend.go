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

package local

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/protobuf/jsonpb"

	"istio.io/istio/pkg/test/fakes/policy"
	"istio.io/istio/pkg/test/framework/environment"
)

type policyBackend struct {
	port       int
	backend    *policy.Backend
	controller *policy.Controller
}

var _ environment.DeployedPolicyBackend = &policyBackend{}

func newPolicyBackend(port int) (*policyBackend, error) {

	backend := policy.NewPolicyBackend(port)

	err := backend.Start()
	if err != nil {
		return nil, err
	}

	controller, err := policy.NewController(fmt.Sprintf(":%d", port))
	if err != nil {
		_ = backend.Close()
		return nil, err
	}

	return &policyBackend{
		port:       port,
		backend:    backend,
		controller: controller,
	}, nil
}

func (p *policyBackend) DenyCheck(t testing.TB, deny bool) {
	t.Helper()

	if err := p.controller.DenyCheck(deny); err != nil {
		t.Fatalf("Error setting DenyCheck: %v", err)
	}
}

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

func (p *policyBackend) CreateConfigSnippet(name string) string {
	return fmt.Sprintf(
		`apiVersion: "config.istio.io/v1alpha2"
kind: bypass
metadata:
  name: %s
  namespace: istio-system
spec:
  backend_address: 127.0.0.1:%d
`, name, p.port)
}
