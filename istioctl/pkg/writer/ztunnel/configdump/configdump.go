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
	"io"
	"text/tabwriter"

	"sigs.k8s.io/yaml"
)

// ConfigWriter is a writer for processing responses from the Ztunnel Admin config_dump endpoint
type ConfigWriter struct {
	Stdout      io.Writer
	ztunnelDump *ZtunnelDump
	FullDump    []byte
}

// Prime loads the config dump into the writer ready for printing
func (c *ConfigWriter) Prime(b []byte) error {
	cd := map[string]interface{}{}
	zDump := &ZtunnelDump{}
	var (
		zw []*ZtunnelWorkload
		zc []*CertsDump
		zp []*ZtunnelPolicy
		zs []*ZtunnelService
	)

	// TODO(fisherxu): migrate this to jsonpb when issue fixed in golang
	// Issue to track -> https://github.com/golang/protobuf/issues/632
	err := json.Unmarshal(b, &cd)
	if err != nil {
		return fmt.Errorf("error unmarshalling config dump response from ztunnel: %v", err)
	}
	// ensure that data gets unmarshalled into the right data type
	for k, v := range cd {
		switch k {
		case "workloads":
			for _, w := range v.(map[string]interface{}) {
				wj, err := json.Marshal(w)
				if err != nil {
					return fmt.Errorf("error marshalling workload: %v", err)
				}
				curr := &ZtunnelWorkload{}
				err = json.Unmarshal(wj, curr)
				if err != nil {
					return fmt.Errorf("error unmarshalling workload: %v", err)
				}
				zw = append(zw, curr)
			}
			zDump.Workloads = zw
		case "certificates":
			for _, c := range v.([]interface{}) {
				cj, err := json.Marshal(c)
				if err != nil {
					return fmt.Errorf("error marshalling certificate: %v", err)
				}
				curr := &CertsDump{}
				err = json.Unmarshal(cj, curr)
				if err != nil {
					return fmt.Errorf("error unmarshalling certificate: %v", err)
				}
				zc = append(zc, curr)
			}
			zDump.Certificates = zc
		case "policies":
			for _, p := range v.([]interface{}) {
				pj, err := json.Marshal(p)
				if err != nil {
					return fmt.Errorf("error marshalling policy: %v", err)
				}
				curr := &ZtunnelPolicy{}
				err = json.Unmarshal(pj, curr)
				if err != nil {
					return fmt.Errorf("error unmarshalling policy: %v", err)
				}
				zp = append(zp, curr)
			}
			zDump.Policies = zp
		case "services":
			for _, s := range v.(map[string]interface{}) {
				sj, err := json.Marshal(s)
				if err != nil {
					return fmt.Errorf("error marshalling service: %v", err)
				}
				curr := &ZtunnelService{}
				err = json.Unmarshal(sj, curr)
				if err != nil {
					return fmt.Errorf("error unmarshalling service: %v", err)
				}
				zs = append(zs, curr)
			}
			zDump.Services = zs
		}
	}
	c.ztunnelDump = zDump
	return nil
}

// PrintBootstrapDump prints just the bootstrap config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintBootstrapDump(outputFormat string) error {
	// TODO
	return nil
}

func (c *ConfigWriter) PrintFullSummary() error {
	_, _ = c.Stdout.Write([]byte("\n"))
	if err := c.PrintWorkloadSummary(WorkloadFilter{Verbose: true}); err != nil {
		return err
	}
	_, _ = c.Stdout.Write([]byte("\n"))
	if err := c.PrintServiceSummary(ServiceFilter{}); err != nil {
		return err
	}
	_, _ = c.Stdout.Write([]byte("\n"))
	if err := c.PrintPolicySummary(PolicyFilter{}); err != nil {
		return err
	}
	_, _ = c.Stdout.Write([]byte("\n"))
	if err := c.PrintSecretSummary(); err != nil {
		return err
	}
	return nil
}

func (c *ConfigWriter) PrintFullDump(outputFormat string) error {
	out := c.FullDump
	if outputFormat == "yaml" {
		var err error
		out, err = yaml.JSONToYAML(out)
		if err != nil {
			return err
		}
	}
	fmt.Fprintln(c.Stdout, string(out))
	return nil
}

// PrintVersionSummary prints version information for Istio and Ztunnel from the config dump
func (c *ConfigWriter) PrintVersionSummary() error {
	// TODO
	return nil
}

// PrintPodRootCAFromDynamicSecretDump prints just pod's root ca from dynamic secret config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintPodRootCAFromDynamicSecretDump() (string, error) {
	// TODO
	return "", nil
}

func (c *ConfigWriter) tabwriter() *tabwriter.Writer {
	w := new(tabwriter.Writer).Init(c.Stdout, 0, 8, 1, ' ', 0)
	return w
}
