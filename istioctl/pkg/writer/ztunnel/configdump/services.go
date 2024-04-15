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

// ServiceFilter is used to pass filter information into service based config writer print functions
type ServiceFilter struct {
	Namespace string
}

// Verify returns true if the passed workload matches the filter fields
func (wf *ServiceFilter) Verify(svc *ZtunnelService) bool {
	if wf.Namespace != "" {
		if !strings.EqualFold(svc.Namespace, wf.Namespace) {
			return false
		}
	}

	return true
}

// PrintServiceSummary prints a summary of the relevant services in the config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintServiceSummary(filter ServiceFilter) error {
	w := c.tabwriter()
	zDump := c.ztunnelDump

	svcs := slices.Filter(maps.Values(zDump.Services), filter.Verify)
	slices.SortFunc(svcs, func(a, b *ZtunnelService) int {
		if r := cmp.Compare(a.Namespace, b.Namespace); r != 0 {
			return r
		}
		if r := cmp.Compare(a.Name, b.Name); r != 0 {
			return r
		}
		return cmp.Compare(a.Hostname, b.Hostname)
	})
	fmt.Fprintln(w, "NAMESPACE\tNAME\tIP\tWAYPOINT")

	for _, svc := range svcs {
		var ip string
		if len(svc.Addresses) > 0 {
			_, ip, _ = strings.Cut(svc.Addresses[0], "/")
		}
		waypoint := serviceWaypointName(svc, zDump.Services)
		fmt.Fprintf(w, "%v\t%v\t%v\t%v\n",
			svc.Namespace, svc.Name, ip, waypoint)
	}
	return w.Flush()
}

// PrintServiceDump prints the relevant services in the config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintServiceDump(filter ServiceFilter, outputFormat string) error {
	zDump := c.ztunnelDump
	svcs := slices.Filter(maps.Values(zDump.Services), filter.Verify)
	slices.SortFunc(svcs, func(a, b *ZtunnelService) int {
		if r := cmp.Compare(a.Namespace, b.Namespace); r != 0 {
			return r
		}
		if r := cmp.Compare(a.Name, b.Name); r != 0 {
			return r
		}
		return cmp.Compare(a.Hostname, b.Hostname)
	})
	out, err := json.MarshalIndent(svcs, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to marshal workloads: %v", err)
	}
	if outputFormat == "yaml" {
		if out, err = yaml.JSONToYAML(out); err != nil {
			return err
		}
	}
	fmt.Fprintln(c.Stdout, string(out))
	return nil
}
