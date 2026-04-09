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

package bugreport

import (
	"testing"

	"github.com/spf13/cobra"

	"istio.io/istio/pkg/test/util/assert"
	config2 "istio.io/istio/tools/bug-report/pkg/config"
)

func TestIncludeFlagCommaHandling(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []*config2.SelectionSpec
	}{
		{
			name: "comma-separated namespaces in single flag",
			args: []string{"--include", "istio-system,istio-gateways"},
			want: []*config2.SelectionSpec{
				{Namespaces: []string{"istio-system", "istio-gateways"}},
			},
		},
		{
			name: "multiple include flags",
			args: []string{"--include", "istio-system", "--include", "istio-gateways"},
			want: []*config2.SelectionSpec{
				{Namespaces: []string{"istio-system"}},
				{Namespaces: []string{"istio-gateways"}},
			},
		},
		{
			name: "comma-separated namespaces with deployment filter",
			args: []string{"--include", "ns1,ns2/istiod"},
			want: []*config2.SelectionSpec{
				{
					Namespaces:  []string{"ns1", "ns2"},
					Deployments: []string{"istiod"},
				},
			},
		},
		{
			name: "full spec with commas in multiple fields",
			args: []string{"--include", "ns1,ns2/d1,d2/p1,p2"},
			want: []*config2.SelectionSpec{
				{
					Namespaces:  []string{"ns1", "ns2"},
					Deployments: []string{"d1", "d2"},
					Pods:        []string{"p1", "p2"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset global state for each test case.
			included = nil
			gConfig = &config2.BugReportConfig{}

			cmd := &cobra.Command{Use: "test"}
			addFlags(cmd, gConfig)
			cmd.SetArgs(tt.args)
			// Parse flags without executing the command.
			if err := cmd.ParseFlags(tt.args); err != nil {
				t.Fatal(err)
			}

			config, err := parseConfig()
			if err != nil {
				t.Fatal(err)
			}

			assert.Equal(t, config.Include, config2.SelectionSpecs(tt.want))
		})
	}
}

func TestExcludeFlagCommaHandling(t *testing.T) {
	// Reset global state.
	included = nil
	excluded = nil
	gConfig = &config2.BugReportConfig{}

	cmd := &cobra.Command{Use: "test"}
	addFlags(cmd, gConfig)
	args := []string{"--exclude", "my-app-ns,my-other-ns"}
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatal(err)
	}

	config, err := parseConfig()
	if err != nil {
		t.Fatal(err)
	}

	// The first exclude entry is the default (IgnoredNamespaces).
	// The second should be our flag value with both namespaces together.
	if len(config.Exclude) < 2 {
		t.Fatalf("expected at least 2 exclude specs, got %d", len(config.Exclude))
	}
	assert.Equal(t, config.Exclude[1].Namespaces, []string{"my-app-ns", "my-other-ns"})
}
