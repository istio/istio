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

	"istio.io/operator/pkg/apis/istio/v1alpha2"
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
		" Default namespace for output IstioControlPlane CustomResource.")
}

func manifestMigrateCmd(rootArgs *rootArgs, mmArgs *manifestMigrateArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Migrates a file containing Helm values to IstioControlPlane format.",
		Long:  "The migrate subcommand is used to migrate a configuration in Helm values format to IstioControlPlane format.",
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) == 0 {
				migrateFromClusterConfig(rootArgs, mmArgs)
			} else {
				migrateFromFiles(rootArgs, args)
			}
		}}
}

func valueFileFilter(path string) bool {
	return filepath.Base(path) == "values.yaml"
}

// migrateFromFiles handles migration for local values.yaml files
func migrateFromFiles(rootArgs *rootArgs, args []string) {
	checkLogsOrExit(rootArgs)

	logAndPrintf(rootArgs, "translating input values.yaml file at: %s to new API", args[0])
	value, err := util.ReadFiles(args[0], valueFileFilter)
	if err != nil {
		logAndFatalf(rootArgs, err.Error())
	}
	translateFunc(rootArgs, []byte(value))
}

// translateFunc translates the input values and output the result
func translateFunc(rootArgs *rootArgs, values []byte) {
	ts, err := translate.NewReverseTranslator(version.NewMinorVersion(1, 3))
	if err != nil {
		logAndFatalf(rootArgs, "error creating values.yaml translator: %s", err.Error())
	}

	valueStruct := v1alpha2.Values{}
	err = yaml.Unmarshal(values, &valueStruct)
	if err != nil {
		logAndFatalf(rootArgs, "error unmarshalling values.yaml into value struct : %s", err.Error())
	}

	isCPSpec, err := ts.TranslateFromValueToSpec(&valueStruct)
	if err != nil {
		logAndFatalf(rootArgs, "error translating values.yaml: %s", err.Error())
	}
	ms := jsonpb.Marshaler{}
	gotString, err := ms.MarshalToString(isCPSpec)
	if err != nil {
		logAndFatalf(rootArgs, "error marshalling translated IstioControlPlaneSpec: %s", err.Error())
	}
	cpYaml, _ := yaml.JSONToYAML([]byte(gotString))
	if err != nil {
		logAndFatalf(rootArgs, "error converting json: %s\n%s", gotString, err.Error())
	}
	fmt.Println(string(cpYaml))
}

// migrateFromClusterConfig handles migration for in cluster config.
func migrateFromClusterConfig(rootArgs *rootArgs, mmArgs *manifestMigrateArgs) {
	checkLogsOrExit(rootArgs)

	logAndPrintf(rootArgs, "translating in cluster specs")

	c := kubectlcmd.New()
	output, stderr, err := c.GetConfig("istio-sidecar-injector", mmArgs.namespace, "jsonpath='{.data.values}'")
	if err != nil {
		logAndFatalf(rootArgs, err.Error())
	}
	if stderr != "" {
		logAndPrintf(rootArgs, "error: %s\n", stderr)
	}
	var value map[string]interface{}
	if len(output) > 1 {
		output = output[1 : len(output)-1]
	}
	err = json.Unmarshal([]byte(output), &value)
	if err != nil {
		logAndFatalf(rootArgs, "error unmarshalling JSON to untyped map %s", err.Error())
	}
	res, err := yaml.Marshal(value)
	if err != nil {
		logAndFatalf(rootArgs, "error marshalling untyped map to YAML: %s", err.Error())
	}
	translateFunc(rootArgs, res)
}
