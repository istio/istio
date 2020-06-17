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

package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/spf13/cobra"
	yaml "gopkg.in/yaml.v2"

	"istio.io/istio/mixer/tools/codegen/pkg/inventory"
)

func withArgs(args []string, errorf func(format string, a ...interface{})) {
	var mappings []string
	var output string
	var mappingFile string

	rootCmd := cobra.Command{
		Use:   "mixgeninventory",
		Short: "Generates mixer adapter inventory source code",
		Long: "Generates mixer adapter inventory source code from an input map of adapter packages to include in the inventory.\n" +
			"Example: mixgeninventory -p prometheus:istio.io/istio/mixer/adapter/prometheus -p stdio:istio.io/istio/mixer/adapter/stdio",
		Run: func(cmd *cobra.Command, args []string) {
			packageMap := make(map[string]string)

			if mappingFile != "" {
				yamlFile, err := ioutil.ReadFile(mappingFile)
				if err != nil {
					errorf("could not read mapping file: %v", err)
					return
				}
				err = yaml.Unmarshal(yamlFile, &packageMap)
				if err != nil {
					errorf("could not unmarshal mapping file: %v", err)
					return
				}
			} else {
				for _, maps := range mappings {
					m := strings.Split(maps, ":")
					if len(m) != 2 {
						errorf("Invalid flag -p %v", mappings)
					}
					packageMap[strings.TrimSpace(m[0])] = strings.TrimSpace(m[1])
				}
			}

			out := os.Stdout
			if len(output) > 0 {
				file, err := os.Create(output)
				if err != nil {
					errorf("could not create output file '%s': %v", output, err)
					return
				}
				out = file
			}

			if err := inventory.Generate(packageMap, out); err != nil {
				errorf("%v", err)
			}
		},
	}

	rootCmd.SetArgs(args)

	rootCmd.PersistentFlags().StringArrayVarP(&mappings, "packages", "p", []string{},
		"colon-separated mapping of Go packages to their full import paths. Example: -p prometheus:istio.io/istio/mixer/adapter/prometheus")

	rootCmd.PersistentFlags().StringVarP(&mappingFile, "file", "f", "",
		"Path to a YAML file that maps Go package names to their full import paths.")

	rootCmd.PersistentFlags().StringVarP(&output, "output", "o", "", "name of file to generate")

	if err := rootCmd.Execute(); err != nil {
		errorf("%v", err)
	}
}

func main() {
	withArgs(os.Args[1:],
		func(format string, a ...interface{}) {
			fmt.Fprintf(os.Stderr, format+"\n", a...) // nolint: gas
			os.Exit(1)
		})
}
