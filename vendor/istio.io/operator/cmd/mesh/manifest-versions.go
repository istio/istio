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
		versionsMapURL, "URI for operator versions to Istio versions map")
}

func manifestVersionsCmd(rootArgs *rootArgs, versionsArgs *manifestVersionsArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "versions",
		Short: "List the versions of Istio recommended for and supported by this version of the operator binary",
		Long:  "List the versions of Istio recommended for and supported by this version of the operator binary.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("versions accepts no positional arguments")
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			l := newLogger(rootArgs.logToStdErr, cmd.OutOrStdout(), cmd.OutOrStderr())
			manifestVersions(rootArgs, versionsArgs, l)
		}}
}

func manifestVersions(args *rootArgs, mvArgs *manifestVersionsArgs, l *logger) {
	initLogsOrExit(args)

	var b []byte
	var err error
	uri := mvArgs.versionsURI

	if strings.HasPrefix(uri, "http") {
		b, err = httprequest.Get(uri)
		if err != nil {
			l.logAndFatal(err.Error())
		}
	} else {
		b, err = ioutil.ReadFile(uri)
		if err != nil {
			l.logAndFatal(err.Error())
		}
	}
	var versions []*version.CompatibilityMapping
	if err = yaml.Unmarshal(b, &versions); err != nil {
		l.logAndFatal(err.Error())
	}

	var myVersionMap *version.CompatibilityMapping
	for _, v := range versions {
		if v.OperatorVersion.Equal(binversion.OperatorBinaryGoVersion) {
			myVersionMap = v
		}
	}

	if myVersionMap == nil {
		l.logAndFatal("This operator version ", binversion.OperatorBinaryGoVersion.String(), " was not found in the global manifestVersions map.")
	}

	fmt.Print("\nOperator version is ", binversion.OperatorBinaryGoVersion.String(), ".\n\n")
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
