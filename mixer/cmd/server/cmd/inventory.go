// Copyright 2017 Istio Authors
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

package cmd

import (
	"sort"

	"github.com/ghodss/yaml"
	"github.com/spf13/cobra"

	"istio.io/mixer/adapter"
	pkgadapter "istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/adapterManager"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/config"
)

func adapterCmd(outf outFn) *cobra.Command {
	adapterCmd := cobra.Command{
		Use:   "inventory",
		Short: "Inventory of available adapters and aspects in the mixer",
	}

	adapterCmd.AddCommand(&cobra.Command{
		Use:   "adapter",
		Short: "List available adapter builders",
		Run: func(cmd *cobra.Command, args []string) {
			listBuilders(outf)
		},
	})

	adapterCmd.AddCommand(&cobra.Command{
		Use:   "aspect",
		Short: "List available aspects",
		Run: func(cmd *cobra.Command, args []string) {
			listAspects(outf)
		},
	})

	return &adapterCmd
}

func listAspects(outf outFn) {
	aspectMap := adapterManager.Aspects(aspect.Inventory())

	keys := []string{}
	for kind := range aspectMap {
		keys = append(keys, kind.String())
	}

	sort.Strings(keys)

	for _, kind := range keys {
		outf("aspect %s\n", kind)
		k, _ := aspect.ParseKind(kind)
		printAspectConfigValidator(outf, aspectMap[k])
	}
}

func listBuilders(outf outFn) {
	builderMap := adapterManager.BuilderMap(adapter.Inventory())
	keys := []string{}
	for k := range builderMap {
		keys = append(keys, k)
	}

	sort.Strings(keys)
	for _, impl := range keys {
		b := builderMap[impl].Builder

		outf("adapter %s: %s\n", impl, b.Description())
		printAdapterConfigValidator(outf, b)
	}
}

func printAdapterConfigValidator(outf outFn, v pkgadapter.ConfigValidator) {
	outf("Params: \n")
	c := v.DefaultConfig()
	if c == nil {
		return
	}
	out, err := yaml.Marshal(c)
	if err != nil {
		outf("%s", err)
	}
	outf("%s\n", string(out[:]))
}

func printAspectConfigValidator(outf outFn, v config.AspectValidator) {
	outf("Params: \n")
	c := v.DefaultConfig()
	if c == nil {
		return
	}
	out, err := yaml.Marshal(c)
	if err != nil {
		outf("%s", err)
	}
	outf("%s\n", string(out[:]))
}
