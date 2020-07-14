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

package v1alpha3

import "testing"

func TestGetClusterNameFromURL(t *testing.T) {
	cluster, err := thriftRLSClusterNameFromAuthority("")
	if err == nil || cluster != "" {
		t.Fatalf("should error and return empty url (got %v)", cluster)
	}
	cluster, err = thriftRLSClusterNameFromAuthority("host.com:80")
	if err != nil {
		t.Fatal("host without port should not cause error")
	}
	if cluster != "outbound|80||host.com" {
		t.Fatalf("Should return correct cluster name (got %v)", cluster)
	}
}
