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
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/spf13/cobra"

	"istio.io/operator/pkg/kubectlcmd"
	"istio.io/operator/pkg/translate"
	"istio.io/operator/pkg/util"
	"istio.io/operator/pkg/version"
)

const (
	defaultNamespace = "istio-system"
)

type manifestMigrateArgs struct {
	// namespace is the namespace to get the in cluster configMap
	namespace string
}

func addManifestMigrateFlags(cmd *cobra.Command, args *manifestMigrateArgs) {
	cmd.PersistentFlags().StringVarP(&args.namespace, "namespace", "n", defaultNamespace,
		" Default namespace for output IstioControlPlane CustomResource")
}

func manifestMigrateCmd(rootArgs *rootArgs, mmArgs *manifestMigrateArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "migrate [<filepath>]",
		Short: "Migrates a file containing Helm values to IstioControlPlane format",
		Long:  "The migrate subcommand migrates a configuration from Helm values format to IstioControlPlane format.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return fmt.Errorf("migrate accepts optional single filepath")
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			l := newLogger(rootArgs.logToStdErr, cmd.OutOrStdout(), cmd.OutOrStderr())
			if len(args) == 0 {
				migrateFromClusterConfig(rootArgs, mmArgs, l)
			} else {
				migrateFromFiles(rootArgs, args, l)
			}
		}}
}

func valueFileFilter(path string) bool {
	return filepath.Base(path) == "values.yaml"
}

// migrateFromFiles handles migration for local values.yaml files
func migrateFromFiles(rootArgs *rootArgs, args []string, l *logger) {
	initLogsOrExit(rootArgs)
	value, err := util.ReadFilesWithFilter(args[0], valueFileFilter)
	if err != nil {
		l.logAndFatal(err.Error())
	}
	if value == "" {
		l.logAndPrint("no valid value.yaml file specified")
		return
	}
	translateFunc([]byte(value), l)
}

// translateFunc translates the input values and output the result
func translateFunc(values []byte, l *logger) {
	ts, err := translate.NewReverseTranslator(version.NewMinorVersion(1, 3))
	if err != nil {
		l.logAndFatal("error creating values.yaml translator: ", err.Error())
	}

	isCPSpec, err := ts.TranslateFromValueToSpec(values)
	if err != nil {
		l.logAndFatal("error translating values.yaml: ", err.Error())
	}
	ms := jsonpb.Marshaler{}
	gotString, err := ms.MarshalToString(isCPSpec)
	if err != nil {
		l.logAndFatal("error marshalling translated IstioControlPlaneSpec: ", err.Error())
	}
	cpYaml, _ := yaml.JSONToYAML([]byte(gotString))
	if err != nil {
		l.logAndFatal("error converting JSON: ", gotString, "\n", err.Error())
	}
	l.print(string(cpYaml) + "\n")
}

// migrateFromClusterConfig handles migration for in cluster config.
func migrateFromClusterConfig(rootArgs *rootArgs, mmArgs *manifestMigrateArgs, l *logger) {
	initLogsOrExit(rootArgs)

	l.logAndPrint("translating in cluster specs\n")

	c := kubectlcmd.New()
	output, stderr, err := c.GetConfig("istio-sidecar-injector", mmArgs.namespace, "jsonpath='{.data.values}'")
	if err != nil {
		l.logAndFatal(err.Error())
	}
	if stderr != "" {
		l.logAndPrint("error: ", stderr, "\n")
	}
	var value map[string]interface{}
	if len(output) > 1 {
		output = output[1 : len(output)-1]
	}
	err = json.Unmarshal([]byte(output), &value)
	if err != nil {
		l.logAndFatal("error unmarshalling JSON to untyped map ", err.Error())
	}
	res, err := yaml.Marshal(value)
	if err != nil {
		l.logAndFatal("error marshalling untyped map to YAML: ", err.Error())
	}
	translateFunc(res, l)
}
