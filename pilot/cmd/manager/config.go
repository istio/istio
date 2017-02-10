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
	"os"
	"sort"

	"github.com/ghodss/yaml"

	"k8s.io/client-go/pkg/api"

	"github.com/golang/protobuf/proto"
	"github.com/spf13/cobra"

	"istio.io/manager/model"
)

var (
	configCmd = &cobra.Command{
		Use:   "config",
		Short: "Istio configuration registry",
		RunE: func(cmd *cobra.Command, args []string) error {
			kinds := make([]string, 0)
			for kind := range model.IstioConfig {
				kinds = append(kinds, kind)
			}
			sort.Strings(kinds)
			fmt.Printf("Available configuration resources: %v\n", kinds)
			return nil
		},
	}

	putCmd = &cobra.Command{
		Use:   "put [kind] [name]",
		Short: "Store a configuration object from standard input YAML",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return fmt.Errorf("Provide kind and name")
			}
			kind, ok := model.IstioConfig[args[0]]
			if !ok {
				return fmt.Errorf("Missing kind %s", args[0])
			}
			if flags.namespace == "" {
				flags.namespace = api.NamespaceDefault
			}

			// read stdin
			bytes, err := ioutil.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("Cannot read input: %v", err)
			}

			out, err := yaml.YAMLToJSON(bytes)
			if err != nil {
				return fmt.Errorf("Cannot read YAML input: %v", err)
			}

			v, err := kind.FromJSON(string(out))
			if err != nil {
				return fmt.Errorf("Cannot parse proto message: %v", err)
			}

			err = flags.client.Put(model.Key{
				Kind:      args[0],
				Name:      args[1],
				Namespace: flags.namespace,
			}, v)

			return err
		},
	}

	getCmd = &cobra.Command{
		Use:   "get [kind] [name]",
		Short: "Retrieve a configuration object",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return fmt.Errorf("Provide kind and name")
			}
			if flags.namespace == "" {
				flags.namespace = api.NamespaceDefault
			}
			item, exists := flags.client.Get(model.Key{
				Kind:      args[0],
				Name:      args[1],
				Namespace: flags.namespace,
			})
			if !exists {
				return fmt.Errorf("Does not exist")
			}
			print(args[0], item)
			return nil
		},
	}

	listCmd = &cobra.Command{
		Use:   "list [kind...]",
		Short: "List configuration objects",
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, kind := range args {
				list, err := flags.client.List(kind, flags.namespace)
				if err != nil {
					fmt.Printf("Error listing %s: %v\n", kind, err)
				} else {
					for key, item := range list {
						fmt.Printf("kind: %s\n", key.Kind)
						fmt.Printf("name: %s\n", key.Name)
						fmt.Printf("namespace: %s\n", key.Namespace)
						print(key.Kind, item)
						fmt.Println("---")
					}
				}
			}
			return nil
		},
	}

	deleteCmd = &cobra.Command{
		Use:   "delete [kind] [name]",
		Short: "Delete a configuration object",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return fmt.Errorf("Provide kind and name")
			}
			if flags.namespace == "" {
				flags.namespace = api.NamespaceDefault
			}
			err := flags.client.Delete(model.Key{
				Kind:      args[0],
				Name:      args[1],
				Namespace: flags.namespace,
			})
			return err
		},
	}
)

func init() {
	configCmd.AddCommand(putCmd)
	configCmd.AddCommand(getCmd)
	configCmd.AddCommand(listCmd)
	configCmd.AddCommand(deleteCmd)
}

func print(kind string, item proto.Message) {
	schema := model.IstioConfig[kind]
	js, err := schema.ToJSON(item)
	if err != nil {
		fmt.Printf("Error converting to JSON: %v", err)
		return
	}
	yml, err := yaml.JSONToYAML([]byte(js))
	if err != nil {
		fmt.Printf("Error converting to YAML: %v", err)
	}
	fmt.Print(string(yml))
}
