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

package structpath_test

import (
	"testing"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pkg/test/util/structpath"
)

func TestContainSubstring(t *testing.T) {
	testResponse := &discovery.DiscoveryResponse{
		VersionInfo: "2019-07-16T10:54:41-07:00/1",
		TypeUrl:     "some.Random.Type.URL",
	}
	validator := structpath.ForProto(testResponse)

	tests := []struct {
		name    string
		substrs []string
		err     bool
	}{
		{
			name:    "Substring exist",
			substrs: []string{"Random", "Type", "URL", "some"},
			err:     false,
		},
		{
			name:    "Substring does not exist",
			substrs: []string{"RaNdOm"},
			err:     true,
		},
		{
			name:    "Substring empty",
			substrs: []string{" "},
			err:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var instance *structpath.Instance
			for _, s := range tt.substrs {
				instance = validator.ContainSubstring(s, "{.typeUrl}")
			}
			err := instance.Check()
			if tt.err && err == nil {
				t.Errorf("expected err but got %v for %s", err, tt.name)
			}
		})
	}
}
