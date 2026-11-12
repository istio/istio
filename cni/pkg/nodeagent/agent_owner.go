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
)

// agentOwnerFileName is written next to the CNI plugin socket by each node agent as it
// starts, before it creates any node state. An agent that is shutting down reads it to
// tell whether a newer agent has already started on the node.
const agentOwnerFileName = "agent-owner"

type agentOwner struct {
	path string
	name string
}

// claimAgentOwner records name as the node's current agent. It is best effort: if the
// file cannot be written the agent behaves as before and never reports itself replaced.
func claimAgentOwner(dir, name string) agentOwner {
	if dir == "" || name == "" {
		return agentOwner{}
	}
	o := agentOwner{path: filepath.Join(dir, agentOwnerFileName), name: name}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		log.Warnf("failed to create %s: %v", dir, err)
		return agentOwner{}
	}
	tmp := o.path + "." + name
	if err := os.WriteFile(tmp, []byte(name), 0o644); err != nil {
		log.Warnf("failed to write %s: %v", tmp, err)
		return agentOwner{}
	}
	if err := os.Rename(tmp, o.path); err != nil {
		_ = os.Remove(tmp)
		log.Warnf("failed to write %s: %v", o.path, err)
		return agentOwner{}
	}
	return o
}

// replaced reports whether another agent has claimed the node since this one did.
func (o agentOwner) replaced() bool {
	if o.path == "" {
		return false
	}
	b, err := os.ReadFile(o.path)
	return err == nil && string(b) != o.name
}

// release removes the claim if it is still this agent's.
func (o agentOwner) release() {
	if o.path == "" || o.replaced() {
		return
	}
	if err := os.Remove(o.path); err != nil && !os.IsNotExist(err) {
		log.Warnf("failed to remove %s: %v", o.path, err)
	}
}
