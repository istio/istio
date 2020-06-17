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
	"reflect"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/fakes/policy"
	"istio.io/istio/pkg/test/util/retry"
)

type client struct {
	controller *policy.Controller
}

// DenyCheck implementation
func (c *client) DenyCheck(t test.Failer, deny bool) {
	t.Helper()

	if err := c.controller.DenyCheck(deny); err != nil {
		t.Fatalf("Error setting DenyCheck: %v", err)
	}
}

// AllowCheck implementation
func (c *client) AllowCheck(t test.Failer, d time.Duration, count int32) {
	t.Helper()

	if err := c.controller.AllowCheck(d, count); err != nil {
		t.Fatalf("Error setting AllowCheck: %v", err)
	}
}

// ExpectReport implementation
func (c *client) ExpectReport(t test.Failer, expected ...proto.Message) {
	t.Helper()

	_, err := retry.Do(func() (interface{}, bool, error) {
		reports, err := c.controller.GetReports()
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
func (c *client) ExpectReportJSON(t test.Failer, expected ...string) {
	t.Helper()

	_, err := retry.Do(func() (interface{}, bool, error) {
		reports, err := c.controller.GetReports()
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
				t.Fatalf("Failed marshaling to string: %v", err)
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

func (c *client) GetReports(t test.Failer) []proto.Message {
	t.Helper()
	reports, err := c.controller.GetReports()
	if err != nil {
		t.Fatalf("PolicyBackend.GetReports: %v", err)
	}

	return reports
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
