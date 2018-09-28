// Copyright 2018 Istio Authors
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

package rbac

import (
	"reflect"
	"testing"

	"istio.io/istio/mixer/template/authorization"
)

func TestUtil_createInstance(t *testing.T) {
	testCases := []struct {
		name     string
		subject  SubjectArgs
		action   ActionArgs
		instance *authorization.Instance
	}{
		{name: "every field set",
			subject: SubjectArgs{
				User: "alice", Groups: "admin", Properties: []string{"key1=value1", "key2=x==y"}},
			action: ActionArgs{
				Namespace: "ns", Service: "svc", Path: "/v1", Method: "GET",
				Properties: []string{"key3=value3", "key4=value4"}},
			instance: &authorization.Instance{
				Subject: &authorization.Subject{
					User: "alice", Groups: "admin",
					Properties: map[string]interface{}{"key1": "value1", "key2": "x==y"}},
				Action: &authorization.Action{
					Namespace: "ns", Service: "svc", Path: "/v1", Method: "GET",
					Properties: map[string]interface{}{"key3": "value3", "key4": "value4"}}}},

		{name: "no = in subject property",
			subject: SubjectArgs{
				User: "alice", Groups: "admin", Properties: []string{"key1=value1", "key2value2"}},
			action: ActionArgs{
				Namespace: "ns", Service: "svc", Path: "/v1", Method: "GET",
				Properties: []string{"key1=value1", "key2=value2"}}},

		{name: "no = in action property",
			subject: SubjectArgs{
				User: "alice", Groups: "admin", Properties: []string{"key1=value1", "key2=value2"}},
			action: ActionArgs{
				Namespace: "ns", Service: "svc", Path: "/v1", Method: "GET",
				Properties: []string{"key1=value1", "key2value2"}}},

		{name: "duplicate property",
			subject: SubjectArgs{
				User: "alice", Groups: "admin", Properties: []string{"key1=value1", "key1=value2"}},
			action: ActionArgs{
				Namespace: "ns", Service: "svc", Path: "/v1", Method: "GET",
				Properties: []string{"key1=value1", "key2=value2"}}},
	}

	for _, tc := range testCases {
		instance, err := createInstance(tc.subject, tc.action)
		if tc.instance != nil {
			if !reflect.DeepEqual(tc.instance, instance) {
				t.Errorf("%s: got %s but want %s", tc.name, instance, tc.instance)
			}
		} else {
			if err == nil {
				t.Errorf("%s: succeeded but want error", tc.name)
			}
		}
	}
}
