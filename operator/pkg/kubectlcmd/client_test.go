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
		name                   string
		namespace              string
		manifest               string
		dryrun                 bool
		args                   []string
		err                    error
		expectArgs             []string
		isstdoutandstderrempty bool
		kubeconfig             string
		context                string
		output                 string
		verbose                bool
		prune                  bool
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
		{
			name:                   "dry run",
			namespace:              "kube-system",
			manifest:               "heynow",
			dryrun:                 true,
			isstdoutandstderrempty: true,
			args:                   []string{},
			expectArgs:             []string{},
		},
		{
			name:                   "empty mainfest",
			namespace:              "kube-system",
			manifest:               "",
			isstdoutandstderrempty: true,
			args:                   []string{},
			expectArgs:             []string{},
		},
		{
			name:       "kubeconfig option",
			namespace:  "kube-system",
			manifest:   "heynow",
			kubeconfig: "test",
			expectArgs: []string{"kubectl", "apply", "--kubeconfig", "test", "-n", "kube-system", "-f", "-"},
		},
		{
			name:       "context option",
			namespace:  "kube-system",
			manifest:   "heynow",
			context:    "test",
			expectArgs: []string{"kubectl", "apply", "--context", "test", "-n", "kube-system", "-f", "-"},
		},
		{
			name:       "output option",
			namespace:  "kube-system",
			manifest:   "heynow",
			output:     "test",
			expectArgs: []string{"kubectl", "apply", "-n", "kube-system", "-o", "test", "-f", "-"},
		},
		{
			name:       "verbose option",
			namespace:  "kube-system",
			manifest:   "heynow",
			verbose:    true,
			expectArgs: []string{"kubectl", "apply", "-n", "kube-system", "-f", "-"},
		},
		{
			name:       "prune option",
			namespace:  "kube-system",
			manifest:   "heynow",
			prune:      true,
			expectArgs: []string{"kubectl", "apply", "-n", "kube-system", "--prune", "-f", "-"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cs := collector{Error: test.err}
			kubectl := &Client{cmdSite: &cs}
			opts := &Options{
				Namespace:  test.namespace,
				ExtraArgs:  test.args,
				DryRun:     test.dryrun,
				Kubeconfig: test.kubeconfig,
				Context:    test.context,
				Output:     test.output,
				Verbose:    test.verbose,
				Prune:      &test.prune,
			}
			_, _, err := kubectl.Apply(test.manifest, opts)

			if test.err != nil && err == nil {
				t.Error("expected error to occur")
			} else if test.err == nil && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if test.isstdoutandstderrempty {
				return
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

func TestKubectlDelete(t *testing.T) {
	tests := []struct {
		name                   string
		namespace              string
		manifest               string
		args                   []string
		err                    error
		expectArgs             []string
		isstdoutandstderrempty bool
	}{
		{
			name:       "manifest",
			namespace:  "",
			manifest:   "foo",
			expectArgs: []string{"kubectl", "delete", "-f", "-"},
		},
		{
			name:       "manifest with delete",
			namespace:  "kube-system",
			manifest:   "heynow",
			expectArgs: []string{"kubectl", "delete", "-n", "kube-system", "-f", "-"},
		},
		{
			name:                   "empty manifest",
			namespace:              "kube-system",
			manifest:               "",
			isstdoutandstderrempty: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cs := collector{Error: test.err}
			kubectl := &Client{cmdSite: &cs}
			opts := &Options{
				Namespace: test.namespace,
				ExtraArgs: test.args,
			}
			_, _, err := kubectl.Delete(test.manifest, opts)

			if test.err != nil && err == nil {
				t.Error("expected error to occur")
			} else if test.err == nil && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if test.isstdoutandstderrempty {
				return
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

func TestKubectlGetAll(t *testing.T) {
	tests := []struct {
		name       string
		namespace  string
		args       []string
		err        error
		expectArgs []string
	}{
		{
			name:       "default",
			namespace:  "",
			expectArgs: []string{"kubectl", "get", "all"},
		},
		{
			name:       "namespace",
			namespace:  "kube-system",
			expectArgs: []string{"kubectl", "get", "all", "-n", "kube-system"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cs := collector{Error: test.err}
			kubectl := &Client{cmdSite: &cs}
			opts := &Options{
				Namespace: test.namespace,
				ExtraArgs: test.args,
			}
			_, _, err := kubectl.GetAll(opts)

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
		})
	}
}

func TestKubectlGetConfig(t *testing.T) {
	tests := []struct {
		name       string
		cmname     string
		namespace  string
		args       []string
		err        error
		expectArgs []string
	}{
		{
			name:       "default",
			cmname:     "foo",
			namespace:  "",
			expectArgs: []string{"kubectl", "get", "cm", "foo"},
		},
		{
			name:       "namespace",
			cmname:     "foo",
			namespace:  "kube-system",
			expectArgs: []string{"kubectl", "get", "cm", "foo", "-n", "kube-system"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cs := collector{Error: test.err}
			kubectl := &Client{cmdSite: &cs}
			opts := &Options{
				Namespace: test.namespace,
				ExtraArgs: test.args,
			}
			_, _, err := kubectl.GetConfigMap(test.cmname, opts)

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
		})
	}
}
