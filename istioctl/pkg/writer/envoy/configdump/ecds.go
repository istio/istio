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
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pkg/util/protomarshal"
)

const typeURLPrefix = "type.googleapis.com/"

func (c *ConfigWriter) PrintEcds(outputFormat string) error {
	if c.configDump == nil {
		return fmt.Errorf("config writer has not been primed")
	}

	dump, err := c.configDump.GetEcdsConfigDump()
	if err != nil {
		return err
	}

	out, err := protomarshal.MarshalIndentWithGlobalTypesResolver(dump, "    ")
	if err != nil {
		return err
	}
	if outputFormat == "yaml" {
		if out, err = yaml.JSONToYAML(out); err != nil {
			return err
		}
	}
	_, _ = fmt.Fprintln(c.Stdout, string(out))
	return nil
}

func (c *ConfigWriter) PrintEcdsSummary() error {
	w := new(tabwriter.Writer).Init(c.Stdout, 0, 8, 5, ' ', 0)

	fmt.Fprintln(w, "ECDS NAME\tTYPE")
	dump, err := c.retrieveSortedEcds()
	if err != nil {
		return err
	}
	for _, ecds := range dump {
		fmt.Fprintf(w, "%v\t%v\n",
			ecds.Name,
			strings.TrimPrefix(ecds.GetTypedConfig().GetTypeUrl(), typeURLPrefix),
		)
	}

	return w.Flush()
}

func (c *ConfigWriter) retrieveSortedEcds() ([]*core.TypedExtensionConfig, error) {
	if c.configDump == nil {
		return nil, fmt.Errorf("config writer has not been primed")
	}

	dump, err := c.configDump.GetEcdsConfigDump()
	if err != nil {
		return nil, err
	}

	ecds := make([]*core.TypedExtensionConfig, 0, len(dump.EcdsFilters))
	for _, config := range dump.GetEcdsFilters() {
		c := &core.TypedExtensionConfig{}
		err := config.GetEcdsFilter().UnmarshalTo(c)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve TypedExtensionConfig: %v", err)
		}

		ecds = append(ecds, c)
	}

	sort.Slice(ecds, func(i, j int) bool {
		if ecds[i].GetTypedConfig().GetTypeUrl() == ecds[j].GetTypedConfig().GetTypeUrl() {
			return ecds[i].GetName() < ecds[j].GetName()
		}

		return ecds[i].GetTypedConfig().GetTypeUrl() < ecds[j].GetTypedConfig().GetTypeUrl()
	})

	return ecds, nil
}
