// Copyright 2019 Istio Authors
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

package kubectlcmd

import (
	"io/ioutil"
	"os/exec"
	"reflect"
	"testing"
)

// collector is a commandSite implementation that stubs cmd.Run() calls for tests
type collector struct {
	Error error
	Cmds  []*exec.Cmd
}

func (s *collector) Run(c *exec.Cmd) error {
	s.Cmds = append(s.Cmds, c)
	return s.Error
}

func TestKubectlApply(t *testing.T) {
	tests := []struct {
		name       string
		namespace  string
		manifest   string
		args       []string
		err        error
		expectArgs []string
	}{
		{
			name:       "manifest",
			namespace:  "",
			manifest:   "foo",
			expectArgs: []string{"kubectl", "apply", "-f", "-"},
		},
		{
			name:       "manifest with apply",
			namespace:  "kube-system",
			manifest:   "heynow",
			expectArgs: []string{"kubectl", "apply", "-n", "kube-system", "-f", "-"},
		},
		{
			name:       "manifest with prune",
			namespace:  "kube-system",
			manifest:   "heynow",
			args:       []string{"--prune=true", "--prune-whitelist=hello-world"},
			expectArgs: []string{"kubectl", "apply", "-n", "kube-system", "--prune=true", "--prune-whitelist=hello-world", "-f", "-"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cs := collector{Error: test.err}
			kubectl := &Client{cmdSite: &cs}
			_, _, err := kubectl.Apply(false, false, test.namespace, test.manifest, test.args...)

			if test.err != nil && err == nil {
				t.Error("expected error to occur")
			} else if test.err == nil && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if len(cs.Cmds) != 1 {
				t.Errorf("expected 1 command to be invoked, got: %d", len(cs.Cmds))
			}

			cmd := cs.Cmds[0]
			if !reflect.DeepEqual(cmd.Args, test.expectArgs) {
				t.Errorf("argument mistmatch, expected: %v, got: %v", test.expectArgs, cmd.Args)
			}

			stdinBytes, err := ioutil.ReadAll(cmd.Stdin)
			if err != nil {
				t.Fatal(err)
			}
			if stdin := string(stdinBytes); stdin != test.manifest {
				t.Errorf("manifest mismatch, expected: %v, got: %v", test.manifest, stdin)
			}
		})
	}

}
