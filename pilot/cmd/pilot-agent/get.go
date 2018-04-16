// Copyright 2018 Istio Authors
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
	"os"

	"github.com/spf13/cobra"
)

var (
	getCmd = &cobra.Command{
		Use:   "get <information-type>",
		Short: "Retrive information about the proxy",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			infoType := args[0]
			switch infoType {
			case "proxyID":
				fmt.Print(getProxyID())
			default:
				return fmt.Errorf("unsupported information type %q", infoType)
			}
			return nil
		},
	}
)

func getProxyID() string {
	podName := os.Getenv("POD_NAME")
	podNamespace := os.Getenv("POD_NAMESPACE")
	return fmt.Sprintf("%v:%v", podName, podNamespace)
}

func init() {
	rootCmd.AddCommand(getCmd)
}
