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

	"github.com/davecgh/go-spew/spew"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/framework/internal"
	"istio.io/istio/pkg/test/util"

	"io"

	"istio.io/istio/pkg/test/fakes/policy"
	"istio.io/istio/pkg/test/framework/environment"
)

var (
	_ environment.DeployedPolicyBackend = &policyBackend{}
	_ io.Closer                         = &policyBackend{}
	_ internal.Resettable               = &policyBackend{}
)

type policyBackend struct {
	address             string
	dependencyNamespace string
	controller          *policy.Controller
	env                 environment.Implementation
	closers             []func() error

	// local only settings
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
`, name, p.backend.Port())
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
func (p *policyBackend) Close() (err error) {
	for _, closer := range p.closers {
		if e := closer(); e != nil {
			err = multierror.Append(err, e)
		}
	}

	return
}

func (p *policyBackend) prependCloser(closer func() error) {
	p.closers = append([]func() error{closer}, p.closers...)
}
