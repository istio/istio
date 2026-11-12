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

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAgentOwner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, agentOwnerFileName)

	old := claimAgentOwner(dir, "istio-cni-node-old")
	if old.replaced() {
		t.Fatal("a sole agent reported itself replaced")
	}

	replacement := claimAgentOwner(dir, "istio-cni-node-new")
	if !old.replaced() {
		t.Fatal("the old agent did not see the replacement")
	}
	if replacement.replaced() {
		t.Fatal("the replacement reported itself replaced")
	}

	old.release()
	if b, err := os.ReadFile(path); err != nil || string(b) != "istio-cni-node-new" {
		t.Fatalf("the old agent's release touched the replacement's claim: %q, %v", b, err)
	}

	replacement.release()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected the claim removed, stat err: %v", err)
	}
	if old.replaced() {
		t.Fatal("a missing claim must not read as replaced")
	}
	if matches, _ := filepath.Glob(path + ".*"); len(matches) != 0 {
		t.Fatalf("temp files left behind: %v", matches)
	}
}

func TestAgentOwnerUnnamed(t *testing.T) {
	dir := t.TempDir()
	o := claimAgentOwner(dir, "")
	if o.replaced() {
		t.Fatal("an agent without a pod name reported itself replaced")
	}
	o.release()
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("an agent without a pod name wrote %v", entries)
	}
}
