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

	goversion "github.com/hashicorp/go-version"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"

	"istio.io/operator/pkg/httprequest"
	"istio.io/operator/pkg/util"
	"istio.io/operator/pkg/version"
	"istio.io/operator/pkg/vfs"
	binversion "istio.io/operator/version"
)

const (
	versionsMapURL = "https://raw.githubusercontent.com/istio/operator/master/data/versions.yaml"
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
		Short: "List the versions of Istio recommended for use or supported for upgrade by this version of the operator binary",
		Long:  "List the versions of Istio recommended for use or supported for upgrade by this version of the operator binary.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("versions accepts no positional arguments")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			l := NewLogger(rootArgs.logToStdErr, cmd.OutOrStdout(), cmd.ErrOrStderr())
			return manifestVersions(rootArgs, versionsArgs, l)
		}}
}

func manifestVersions(args *rootArgs, mvArgs *manifestVersionsArgs, l *Logger) error {
	initLogsOrExit(args)

	myVersionMap, err := getVersionCompatibleMap(mvArgs.versionsURI, binversion.OperatorBinaryGoVersion, l)
	if err != nil {
		return fmt.Errorf("failed to retrieve version map, error: %v", err)
	}

	fmt.Print("\nOperator version is ", binversion.OperatorBinaryGoVersion.String(), ".\n\n")
	fmt.Println("The following installation package versions are recommended for use with this version of the operator:")
	for _, v := range myVersionMap.RecommendedIstioVersions {
		fmt.Printf("  %s\n", v.String())
	}
	fmt.Println("\nThe following installation package versions are supported for upgrade by this version of the operator:")
	for _, v := range myVersionMap.SupportedIstioVersions {
		fmt.Printf("  %s\n", v.String())
	}
	fmt.Println()

	return nil
}

func getVersionCompatibleMap(versionsURI string, binVersion *goversion.Version,
	l *Logger) (*version.CompatibilityMapping, error) {
	var b []byte
	var err error

	b, err = loadCompatibleMapFile(versionsURI, l)
	if err != nil {
		return nil, err
	}

	var versions []*version.CompatibilityMapping
	if err = yaml.Unmarshal(b, &versions); err != nil {
		return nil, err
	}

	var myVersionMap, closestVersionMap *version.CompatibilityMapping
	for _, v := range versions {
		if v.OperatorVersion.Equal(binVersion) {
			myVersionMap = v
			break
		}
		if v.OperatorVersionRange.Check(binVersion) {
			myVersionMap = v
		}
		if (closestVersionMap == nil || v.OperatorVersion.GreaterThan(closestVersionMap.OperatorVersion)) &&
			v.OperatorVersion.LessThanOrEqual(binVersion) {
			closestVersionMap = v
		}
	}

	if myVersionMap == nil {
		myVersionMap = closestVersionMap
	}

	if myVersionMap == nil {
		return nil, fmt.Errorf("this operator version %s was not found in the version map", binVersion.String())
	}
	return myVersionMap, nil
}

func loadCompatibleMapFile(versionsURI string, l *Logger) ([]byte, error) {
	var err error
	if util.IsHTTPURL(versionsURI) {
		if b, err := httprequest.Get(versionsURI); err == nil {
			return b, nil
		}
	} else {
		if b, err := ioutil.ReadFile(versionsURI); err == nil {
			return b, nil
		}
	}

	l.logAndPrintf("Warning: failed to retrieve the version map from (%s): %s. "+
		"Falling back to the internal version map.", versionsURI, err)
	return vfs.ReadFile("versions.yaml")
}
