package configdump

import (
	"cmp"
	"encoding/json"
	"fmt"
	"strings"

	"sigs.k8s.io/yaml"

	"istio.io/istio/pkg/maps"
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

	pols := slices.Filter(maps.Values(zDump.Policies), filter.Verify)
	slices.SortFunc(pols, func(a, b *ZtunnelPolicy) int {
		if r := cmp.Compare(a.Namespace, b.Namespace); r != 0 {
			return r
		}
		return cmp.Compare(a.Name, b.Name)
	})
	fmt.Fprintln(w, "NAMESPACE\tNAME\tACTION\tSCOPE")

	for _, pol := range pols {
		fmt.Fprintf(w, "%v\t%v\t%v\t%v\n",
			pol.Namespace, pol.Name, pol.Action, pol.Scope)
	}
	return w.Flush()
}

// PrintPolicyDump prints the relevant services in the config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintPolicyDump(filter PolicyFilter, outputFormat string) error {
	zDump := c.ztunnelDump
	policies := slices.Filter(maps.Values(zDump.Policies), filter.Verify)
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
