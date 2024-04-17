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
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

// WorkloadFilter is used to pass filter information into workload based config writer print functions
type WorkloadFilter struct {
	Address   string
	Node      string
	Verbose   bool
	Namespace string
}

// Verify returns true if the passed workload matches the filter fields
func (wf *WorkloadFilter) Verify(workload *ZtunnelWorkload) bool {
	if wf.Address == "" && wf.Node == "" && wf.Namespace == "" {
		return true
	}

	if wf.Namespace != "" {
		if !strings.EqualFold(workload.Namespace, wf.Namespace) {
			return false
		}
	}

	if wf.Address != "" {
		var find bool
		for _, ip := range workload.WorkloadIPs {
			if strings.EqualFold(ip, wf.Address) {
				find = true
				break
			}
		}
		if !find {
			return false
		}
	}
	if wf.Node != "" && !strings.EqualFold(workload.Node, wf.Node) {
		return false
	}
	return true
}

// PrintWorkloadSummary prints a summary of the relevant listeners in the config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintWorkloadSummary(filter WorkloadFilter) error {
	w := c.tabwriter()
	zDump := c.ztunnelDump

	verifiedWorkloads := make([]*ZtunnelWorkload, 0, len(zDump.Workloads))
	for _, wl := range zDump.Workloads {
		if filter.Verify(wl) {
			verifiedWorkloads = append(verifiedWorkloads, wl)
		}
	}

	// Sort by name, node
	sort.Slice(verifiedWorkloads, func(i, j int) bool {
		in := verifiedWorkloads[i].Namespace + "." + verifiedWorkloads[i].Name
		jn := verifiedWorkloads[j].Namespace + "." + verifiedWorkloads[j].Name
		if in != jn {
			return in < jn
		}
		iNode := verifiedWorkloads[i].Node
		jNode := verifiedWorkloads[j].Node
		return iNode < jNode
	})

	if filter.Verbose {
		fmt.Fprintln(w, "NAMESPACE\tNAME\tIP\tNODE\tWAYPOINT\tPROTOCOL")
	} else {
		fmt.Fprintln(w, "NAMESPACE\tNAME\tIP\tNODE")
	}

	for _, wl := range verifiedWorkloads {
		var ip string
		if len(wl.WorkloadIPs) > 0 {
			ip = wl.WorkloadIPs[0]
		}
		if filter.Verbose {
			waypoint := waypointName(wl, zDump.Services)
			fmt.Fprintf(w, "%v\t%v\t%v\t%v\t%v\t%v\n",
				wl.Namespace, wl.Name, ip, wl.Node, waypoint, wl.Protocol)
		} else {
			fmt.Fprintf(w, "%v\t%v\t%v\t%v\n", wl.Namespace, wl.Name, ip, wl.Node)
		}
	}
	return w.Flush()
}

// PrintWorkloadDump prints the relevant workloads in the config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintWorkloadDump(filter WorkloadFilter, outputFormat string) error {
	zDump := c.ztunnelDump
	filteredWorkloads := []*ZtunnelWorkload{}
	for _, workload := range zDump.Workloads {
		if filter.Verify(workload) {
			filteredWorkloads = append(filteredWorkloads, workload)
		}
	}
	out, err := json.MarshalIndent(filteredWorkloads, "", "    ")
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

func waypointName(wl *ZtunnelWorkload, services map[string]*ZtunnelService) string {
	if wl.Waypoint == nil {
		return "None"
	}

	if svc, ok := services[wl.Waypoint.Destination]; ok {
		return svc.Name
	}

	return "NA" // Shouldn't normally reach here
}

func serviceWaypointName(svc *ZtunnelService, services map[string]*ZtunnelService) string {
	if svc.Waypoint == nil {
		return "None"
	}

	if svc, ok := services[svc.Waypoint.Destination]; ok {
		return svc.Name
	}

	return "NA" // Shouldn't normally reach here
}
