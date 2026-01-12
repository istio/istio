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

package nodeagent

import "testing"

func TestOpenNetns(t *testing.T) {
	ns, err := OpenNetns("/proc/self/ns/net")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// the inode for netns is proc dynamic, so it needs to be higher than
	// #define PROC_DYNAMIC_FIRST 0xF0000000U

	if ns.Inode() < 0xF0000000 {
		t.Fatalf("unexpected inode: %v", ns.Inode())
	}
	defer ns.Close()
}
