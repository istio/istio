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

package configdump

import (
	"cmp"
	"encoding/json"
	"fmt"
	"strings"

	"sigs.k8s.io/yaml"

	"istio.io/istio/pkg/slices"
)

// PolicyFilter is used to pass filter information into service based config writer print functions
type PolicyFilter struct {
	Namespace string
}

// Verify returns true if the passed workload matches the filter fields
func (wf *PolicyFilter) Verify(pol *ZtunnelPolicy) bool {
	if wf.Namespace != "" {
		if !strings.EqualFold(pol.Namespace, wf.Namespace) {
			return false
		}
	}

	return true
}

// PrintServiceSummary prints a summary of the relevant services in the config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintPolicySummary(filter PolicyFilter) error {
	w := c.tabwriter()
	zDump := c.ztunnelDump

	pols := slices.Filter(zDump.Policies, filter.Verify)
	slices.SortFunc(pols, func(a, b *ZtunnelPolicy) int {
		if r := cmp.Compare(a.Namespace, b.Namespace); r != 0 {
			return r
		}
		return cmp.Compare(a.Name, b.Name)
	})
	fmt.Fprintln(w, "NAMESPACE\tPOLICY NAME\tACTION\tSCOPE\tHITS\tMISSES")

	for _, pol := range pols {
		fmt.Fprintf(w, "%v\t%v\t%v\t%v\t%v\t%v\n",
			pol.Namespace, pol.Name, pol.Action, pol.Scope, pol.Stats.Hits, pol.Stats.Misses)
	}
	return w.Flush()
}

// PrintPolicyDump prints the relevant services in the config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintPolicyDump(filter PolicyFilter, outputFormat string) error {
	zDump := c.ztunnelDump
	policies := slices.Filter(zDump.Policies, filter.Verify)
	slices.SortFunc(policies, func(a, b *ZtunnelPolicy) int {
		if r := cmp.Compare(a.Namespace, b.Namespace); r != 0 {
			return r
		}
		return cmp.Compare(a.Name, b.Name)
	})
	out, err := json.MarshalIndent(policies, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to marshal policies: %v", err)
	}
	if outputFormat == "yaml" {
		if out, err = yaml.JSONToYAML(out); err != nil {
			return err
		}
	}
	fmt.Fprintln(c.Stdout, string(out))
	return nil
}
