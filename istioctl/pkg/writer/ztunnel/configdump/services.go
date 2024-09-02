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

	workloadsByUID := slices.GroupUnique(zDump.Workloads, func(t *ZtunnelWorkload) string {
		return t.UID
	})
	svcs := slices.Filter(zDump.Services, filter.Verify)
	slices.SortFunc(svcs, func(a, b *ZtunnelService) int {
		if r := cmp.Compare(a.Namespace, b.Namespace); r != 0 {
			return r
		}
		if r := cmp.Compare(a.Name, b.Name); r != 0 {
			return r
		}
		return cmp.Compare(a.Hostname, b.Hostname)
	})
	fmt.Fprintln(w, "NAMESPACE\tSERVICE NAME\tSERVICE VIP\tWAYPOINT\tENDPOINTS")

	for _, svc := range svcs {
		ips := []string{}
		for _, addr := range svc.Addresses {
			_, ip, _ := strings.Cut(addr, "/")
			ips = append(ips, ip)
		}
		allEndpoints := len(svc.Endpoints)
		healthyEndpoints := 0
		for _, ep := range svc.Endpoints {
			w, f := workloadsByUID[ep.WorkloadUID]
			if !f {
				continue
			}
			if w.Status != "Healthy" {
				continue
			}
			healthyEndpoints++
		}
		endpoints := fmt.Sprintf("%d/%d", healthyEndpoints, allEndpoints)

		waypoint := serviceWaypointName(svc, zDump.Services)
		fmt.Fprintf(w, "%v\t%v\t%v\t%v\t%v\n",
			svc.Namespace, svc.Name, strings.Join(ips, ","), waypoint, endpoints)
	}
	return w.Flush()
}

// PrintServiceDump prints the relevant services in the config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintServiceDump(filter ServiceFilter, outputFormat string) error {
	zDump := c.ztunnelDump
	svcs := slices.Filter(zDump.Services, filter.Verify)
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
