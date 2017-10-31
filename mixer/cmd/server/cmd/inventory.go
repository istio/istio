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

	"istio.io/istio/mixer/cmd/shared"
	pkgadapter "istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/adapterManager"
	"istio.io/istio/mixer/pkg/aspect"
	"istio.io/istio/mixer/pkg/config"
)

func adapterCmd(legacyAdapters []pkgadapter.RegisterFn, printf shared.FormatFn) *cobra.Command {
	adapterCmd := cobra.Command{
		Use:   "inventory",
		Short: "InventoryLegacy of available adapters and aspects in Mixer",
	}

	adapterCmd.AddCommand(&cobra.Command{
		Use:   "adapter",
		Short: "List available adapter builders",
		Run: func(cmd *cobra.Command, args []string) {
			listBuilders(legacyAdapters, printf)
		},
	})

	adapterCmd.AddCommand(&cobra.Command{
		Use:   "aspect",
		Short: "List available aspects",
		Run: func(cmd *cobra.Command, args []string) {
			listAspects(printf)
		},
	})

	return &adapterCmd
}

func listAspects(printf shared.FormatFn) {
	aspectMap := adapterManager.Aspects(aspect.Inventory())

	keys := []string{}
	for i := 0; i < len(aspectMap); i++ {
		keys = append(keys, aspectMap[i].Kind().String())
	}

	sort.Strings(keys)

	for _, kind := range keys {
		printf("aspect %s", kind)
		k, _ := config.ParseKind(kind)
		printAspectConfigValidator(printf, aspectMap[k])
	}
}

func listBuilders(legacyAdapters []pkgadapter.RegisterFn, printf shared.FormatFn) {
	builderMap := adapterManager.BuilderMap(legacyAdapters)
	keys := []string{}
	for k := range builderMap {
		keys = append(keys, k)
	}

	sort.Strings(keys)
	for _, impl := range keys {
		b := builderMap[impl].Builder

		printf("adapter %s: %s", impl, b.Description())
		printAdapterConfigValidator(printf, b)
	}
}

func printAdapterConfigValidator(printf shared.FormatFn, v pkgadapter.ConfigValidator) {
	printf("Params:")
	c := v.DefaultConfig()
	if c == nil {
		return
	}
	out, err := yaml.Marshal(c)
	if err != nil {
		printf("%s", err)
	}
	printf("%s", string(out[:]))
}

func printAspectConfigValidator(printf shared.FormatFn, v config.AspectValidator) {
	printf("Params:")
	c := v.DefaultConfig()
	if c == nil {
		return
	}
	out, err := yaml.Marshal(c)
	if err != nil {
		printf("%s", err)
	}
	printf("%s", string(out[:]))
}
