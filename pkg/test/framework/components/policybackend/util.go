// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package policybackend

import (
	"encoding/json"
	"reflect"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"

	"istio.io/istio/pkg/test"
)

// ContainsReportJSON checks whether the given proto array contains the expected string
func ContainsReportJSON(t test.Failer, reports []proto.Message, expected string) bool {
	t.Helper()

	e, err := jsonStringToMap(expected)
	if err != nil {
		t.Fatalf("ContainsReportJSON: error converting expected string to JSON: %v", err)
	}

	for _, r := range reports {
		a, err := toMap(r)
		if err != nil {
			t.Fatalf("ContainsReportJSON: error converting proto to JSON: %v", err)
		}

		if reflect.DeepEqual(a, e) {
			return true
		}
	}

	return false
}

func toMap(p proto.Message) (map[string]interface{}, error) {

	m := jsonpb.Marshaler{
		Indent: "  ",
	}

	as, err := m.MarshalToString(p)
	if err != nil {
		return nil, err
	}

	return jsonStringToMap(as)
}

func jsonStringToMap(s string) (map[string]interface{}, error) {
	i := make(map[string]interface{})
	if err := json.Unmarshal([]byte(s), &i); err != nil {
		return nil, err
	}

	return i, nil
}

func jsonStringsToMaps(t test.Failer, arr []string) []map[string]interface{} {
	var result []map[string]interface{}

	for _, a := range arr {
		m, err := jsonStringToMap(a)
		if err != nil {
			t.Fatalf("Error unmarshaling JSON: %v", err)
		}

		result = append(result, m)
	}

	return result
}
