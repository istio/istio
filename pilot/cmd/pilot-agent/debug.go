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

package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"path/filepath"

	"istio.io/istio/pkg/log"

	"github.com/spf13/cobra"
)

var (
	configTypes = map[string]struct{}{
		"all":       {},
		"clusters":  {},
		"listeners": {},
		"routes":    {},
		"static":    {},
	}

	debugCmd = &cobra.Command{
		Use:   "debug <configuration-type>",
		Short: "Debug local envoy",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			configType := args[0]
			if err := validateConfigType(configType); err != nil {
				return err
			}

			if configType == "static" {
				return printStaticConfig()
			} else if configType == "all" {
				for ct := range configTypes {
					switch ct {
					case "clusters", "listeners", "routes":
						if err := printDynamicConfig(ct); err != nil {
							return err
						}
					case "static":
						return printStaticConfig()
					}
				}
				return nil
			}
			return printDynamicConfig(configType)
		},
	}
)

func printStaticConfig() error {
	dir := "/etc/istio/proxy"
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("error reading default config directory: %v", err)
	}
	for _, f := range files {
		filePath := filepath.Join(dir, f.Name())
		contents, err := ioutil.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("error reading config file %q: %v", filePath, err)
		}
		fmt.Println(string(contents))
	}
	return nil
}

func printDynamicConfig(typ string) error {
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:15000/%s", typ))
	if err != nil {
		return err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Errorf("Error closing response body: %v", err)
		}
	}()
	bytes, _ := ioutil.ReadAll(resp.Body)
	fmt.Println(string(bytes))
	return nil
}

func validateConfigType(typ string) error {
	if _, ok := configTypes[typ]; !ok {
		return fmt.Errorf("%q is not a supported debugging config type", typ)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(debugCmd)
}
