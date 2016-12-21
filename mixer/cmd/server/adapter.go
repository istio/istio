// Copyright 2016 Google Inc.
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

	"github.com/spf13/cobra"

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/server"
)

func adapterCmd(errorf errorFn) *cobra.Command {
	adapterCmd := cobra.Command{
		Use:   "adapter",
		Short: "Diagnostics for the available mixer adapters",
	}

	listCmd := cobra.Command{
		Use:   "list",
		Short: "List available adapter builders",
		Run: func(cmd *cobra.Command, args []string) {
			err := listBuilders()
			if err != nil {
				errorf("%v", err)
			}
		},
	}
	adapterCmd.AddCommand(&listCmd)

	return &adapterCmd
}

func printBuilders(listName string, builders map[string]adapter.Builder) {
	fmt.Printf("%s\n", listName)
	if len(builders) > 0 {
		for _, b := range builders {
			fmt.Printf("  %s: %s\n", b.Name(), b.Description())
		}
	} else {
		fmt.Printf("  <none>\n")
	}

	fmt.Printf("\n")
}

func listBuilders() error {
	mgr, err := server.NewAdapterManager()
	if err != nil {
		return fmt.Errorf("Unable to initialize adapters: %v", err)
	}

	printBuilders("List Checkers", mgr.ListCheckers)
	printBuilders("Loggers", mgr.Loggers)

	return nil
}
