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

	"istio.io/istio/istioctl/pkg/util/configdump"
)

// ConfigWriter is a writer for processing responses from the Ztunnel Admin config_dump endpoint
type ConfigWriter struct {
	Stdout      io.Writer
	ztunnelDump *configdump.ZtunnelDump
}

// Prime loads the config dump into the writer ready for printing
func (c *ConfigWriter) Prime(b []byte) error {
	cd := configdump.ZtunnelDump{}
	// TODO(fisherxu): migrate this to jsonpb when issue fixed in golang
	// Issue to track -> https://github.com/golang/protobuf/issues/632
	err := json.Unmarshal(b, &cd)
	if err != nil {
		return fmt.Errorf("error unmarshalling config dump response from ztunnel: %v", err)
	}
	c.ztunnelDump = &cd
	return nil
}

// PrintBootstrapDump prints just the bootstrap config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintBootstrapDump(outputFormat string) error {
	// TODO
	return nil
}

// PrintSecretDump prints just the secret config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintSecretDump(outputFormat string) error {
	// TODO
	return nil
}

// PrintSecretSummary prints a summary of dynamic active secrets from the config dump
func (c *ConfigWriter) PrintSecretSummary() error {
	// TODO
	return nil
}

func (c *ConfigWriter) PrintFullSummary(wf WorkloadFilter) error {
	_, _ = c.Stdout.Write([]byte("\n"))
	if err := c.PrintWorkloadSummary(wf); err != nil {
		return err
	}
	_, _ = c.Stdout.Write([]byte("\n"))
	if err := c.PrintSecretSummary(); err != nil {
		return err
	}
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
