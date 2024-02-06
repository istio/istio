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
	"text/tabwriter"

	"sigs.k8s.io/yaml"

	ztunnelDump "istio.io/istio/istioctl/pkg/util/configdump"
)

// WorkloadFilter is used to pass filter information into workload based config writer print functions
type WorkloadFilter struct {
	Address   string
	Node      string
	Verbose   bool
	Namespace string
}

// Verify returns true if the passed workload matches the filter fields
func (wf *WorkloadFilter) Verify(workload *ztunnelDump.ZtunnelWorkload) bool {
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
	w, zDump, err := c.setupWorkloadConfigWriter()
	if err != nil {
		return err
	}

	verifiedWorkloads := make([]*ztunnelDump.ZtunnelWorkload, 0, len(zDump.Workloads))
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
		fmt.Fprintln(w, "NAMESPACE\tNAME\tNETWORK\tIP\tNODE\tWAYPOINT\tPROTOCOL")
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
			fmt.Fprintf(w, "%v\t%v\t%v\t%v\t%v\t%v\t%v\n",
				wl.Namespace, wl.Name, wl.Network, ip, wl.Node, waypoint, wl.Protocol)
		} else {
			fmt.Fprintf(w, "%v\t%v\t%v\t%v\n", wl.Namespace, wl.Name, ip, wl.Node)
		}
	}
	return w.Flush()
}

// PrintWorkloadDump prints the relevant workloads in the config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintWorkloadDump(filter WorkloadFilter, outputFormat string) error {
	_, zDump, err := c.setupWorkloadConfigWriter()
	if err != nil {
		return err
	}
	filteredWorkloads := []*ztunnelDump.ZtunnelWorkload{}
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

func (c *ConfigWriter) setupWorkloadConfigWriter() (*tabwriter.Writer, *ztunnelDump.ZtunnelDump, error) {
	listeners, err := c.retrieveSortedWorkloadSlice()
	if err != nil {
		return nil, nil, err
	}
	w := new(tabwriter.Writer).Init(c.Stdout, 0, 8, 1, ' ', 0)
	return w, listeners, nil
}

func (c *ConfigWriter) retrieveSortedWorkloadSlice() (*ztunnelDump.ZtunnelDump, error) {
	if c.ztunnelDump == nil {
		return nil, fmt.Errorf("config writer has not been primed")
	}
	workloadDump := c.ztunnelDump
	if workloadDump == nil {
		return nil, fmt.Errorf("workload dump empty")
	}
	if len(workloadDump.Workloads) == 0 {
		return nil, fmt.Errorf("no workloads found")
	}

	return workloadDump, nil
}

func waypointName(wl *ztunnelDump.ZtunnelWorkload, services map[string]*ztunnelDump.ZtunnelService) string {
	if wl.Waypoint == nil {
		return "None"
	}

	if svc, ok := services[wl.Waypoint.Destination]; ok {
		return svc.Name
	}

	return "NA" // Shouldn't normally reach here
}
