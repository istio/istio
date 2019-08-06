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
	"io/ioutil"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"

	"istio.io/operator/pkg/httprequest"
	"istio.io/operator/pkg/version"
	binversion "istio.io/operator/version"
)

const (
	versionsMapURL = "https://raw.githubusercontent.com/istio/operator/master/version/versions.yaml"
)

type manifestVersionsArgs struct {
	// versionsURI is a URI pointing to a YAML formatted versions mapping.
	versionsURI string
}

func addManifestVersionsFlags(cmd *cobra.Command, mvArgs *manifestVersionsArgs) {
	cmd.PersistentFlags().StringVarP(&mvArgs.versionsURI, "versionsURI", "u",
		versionsMapURL, "URI for operator versions to Istio versions map.")
}

func manifestVersionsCmd(rootArgs *rootArgs, versionsArgs *manifestVersionsArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "versions",
		Short: "List the version of Istio recommended for and supported by this version of the operator binary.",
		Long:  "List the version of Istio recommended for and supported by this version of the operator binary.",
		Args:  cobra.ExactArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			manifestVersions(rootArgs, versionsArgs)
		}}

}

func manifestVersions(args *rootArgs, mvArgs *manifestVersionsArgs) {
	checkLogsOrExit(args)

	var b []byte
	var err error
	uri := mvArgs.versionsURI

	if strings.HasPrefix(uri, "http") {
		b, err = httprequest.Get(uri)
		if err != nil {
			logAndFatalf(args, err.Error())
		}
	} else {
		b, err = ioutil.ReadFile(uri)
		if err != nil {
			logAndFatalf(args, err.Error())
		}
	}
	var versions []*version.CompatibilityMapping
	if err = yaml.Unmarshal(b, &versions); err != nil {
		logAndFatalf(args, err.Error())
	}

	var myVersionMap *version.CompatibilityMapping
	for _, v := range versions {
		if v.OperatorVersion.Equal(binversion.OperatorBinaryGoVersion) {
			myVersionMap = v
		}
	}

	if myVersionMap == nil {
		logAndFatalf(args, "This operator version (%s) was not found in the global manifestVersions map.", binversion.OperatorBinaryGoVersion.String())
	}

	fmt.Printf("\nOperator version is %s.\n\n", binversion.OperatorBinaryGoVersion.String())
	fmt.Println("The following installation package versions are recommended for use with this version of the operator:")
	for _, v := range myVersionMap.RecommendedIstioVersions {
		fmt.Printf("  %s\n", v.String())
	}
	fmt.Println("\nThe following installation package versions are supported by this version of the operator:")
	for _, v := range myVersionMap.SupportedIstioVersions {
		fmt.Printf("  %s\n", v.String())
	}
	fmt.Println()
}
