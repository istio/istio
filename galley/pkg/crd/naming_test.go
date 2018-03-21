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

package crd

import "testing"

func TestGroupOf(t *testing.T) {
	tests := map[string]string{
		"foo":         "",
		"foo.bar":     "bar",
		"foo.bar.baz": "bar.baz",
	}

	for k, v := range tests {
		t.Run(k, func(tt *testing.T) {
			if GroupOf(k) != v {
				tt.Fatalf("Unexpected result for %s: got:%s, wanted:%s", k, GroupOf(k), v)
			}
		})
	}
}

func TestResourceOf(t *testing.T) {
	tests := map[string]string{
		"foo":         "foo",
		"foo.bar":     "foo",
		"foo.bar.baz": "foo",
	}

	for k, v := range tests {
		t.Run(k, func(tt *testing.T) {
			if ResourceOf(k) != v {
				tt.Fatalf("Unexpected result for %s: got:%s, wanted:%s", k, ResourceOf(k), v)
			}
		})
	}
}
