// Copyright 2019 Istio Authors
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

package mesh

import (
	"fmt"

	"github.com/spf13/cobra"

	"istio.io/istio/operator/pkg/version"
	binversion "istio.io/istio/operator/version"
)

type manifestVersionsArgs struct {
	// versionsURI is a URI pointing to a YAML formatted versions mapping.
	versionsURI string
}

func addManifestVersionsFlags(cmd *cobra.Command, mvArgs *manifestVersionsArgs) {
	cmd.PersistentFlags().StringVarP(&mvArgs.versionsURI, "versionsURI", "u",
		"", "URI for operator versions to Istio versions map")
}

func manifestVersionsCmd(rootArgs *rootArgs, versionsArgs *manifestVersionsArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "versions",
		Short: "List the versions of Istio recommended for use or supported for upgrade by this version of istioctl",
		Long:  "List the versions of Istio recommended for use or supported for upgrade by this version of istioctl.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("versions accepts no positional arguments")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return manifestVersions(rootArgs, versionsArgs)
		}}
}

func manifestVersions(args *rootArgs, mvArgs *manifestVersionsArgs) error {
	initLogsOrExit(args)

	myVersionMap, err := version.GetVersionCompatibleMap(mvArgs.versionsURI, binversion.OperatorBinaryGoVersion)
	if err != nil {
		return fmt.Errorf("failed to retrieve version map, error: %v", err)
	}

	fmt.Print("\nOperator version is ", binversion.OperatorBinaryGoVersion.String(), ".\n\n")
	fmt.Println("This version of istioctl can:")
	for _, v := range myVersionMap.RecommendedIstioVersions {
		fmt.Printf("  Install Istio %s\n", v.String())
	}
	for _, v := range myVersionMap.SupportedIstioVersions {
		fmt.Printf("  Update Istio from %s to %s\n", v.String(), myVersionMap.RecommendedIstioVersions)
	}
	fmt.Println()

	return nil
}
