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

	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	icpv1alpha2 "istio.io/istio/operator/pkg/apis/istio/v1alpha2"
	"istio.io/istio/operator/pkg/kubectlcmd"
	"istio.io/istio/operator/pkg/translate"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/validate"
	binversion "istio.io/istio/operator/version"
	"istio.io/pkg/log"
)

const (
	defaultNamespace = "istio-system"
)

type manifestMigrateArgs struct {
	// namespace is the namespace to get the in cluster configMap
	namespace string
	// force proceeds even if there are validation errors
	force bool
}

func addManifestMigrateFlags(cmd *cobra.Command, args *manifestMigrateArgs) {
	cmd.PersistentFlags().StringVarP(&args.namespace, "namespace", "n", defaultNamespace,
		" Default namespace for output IstioOperator CustomResource")
	cmd.PersistentFlags().BoolVar(&args.force, "force", false, "Proceed even with validation errors")
}

func manifestMigrateCmd(rootArgs *rootArgs, mmArgs *manifestMigrateArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "migrate [<filepath>]",
		Short: "Migrates a file containing Helm values or IstioControlPlane to IstioOperator format",
		Long:  "The migrate subcommand migrates a configuration from Helm values or IstioControlPlane format to IstioOperator format.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return fmt.Errorf("migrate accepts optional single filepath")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			l := NewLogger(rootArgs.logToStdErr, cmd.OutOrStdout(), cmd.ErrOrStderr())

			if len(args) == 0 {
				return migrateFromClusterConfig(rootArgs, mmArgs, l)
			}

			return migrateFromFiles(rootArgs, mmArgs, args, l)
		}}
}

func valueFileFilter(path string) bool {
	return filepath.Base(path) == "values.yaml" || filepath.Base(path) == "global.yaml"
}

// migrateFromFiles handles migration for local values.yaml files
func migrateFromFiles(rootArgs *rootArgs, mmArgs *manifestMigrateArgs, args []string, l *Logger) error {
	initLogsOrExit(rootArgs)
	value, err := util.ReadFilesWithFilter(args[0], valueFileFilter)
	if err != nil {
		return err
	}
	if value == "" {
		l.logAndPrint("no valid value.yaml file specified")
		return nil
	}
	return translateFunc([]byte(value), mmArgs.force, l)
}

// translateFunc translates the input values and output the result
func translateFunc(values []byte, force bool, l *Logger) error {
	// First, try to translate from IstioControlPlane format.
	icp := &icpv1alpha2.IstioControlPlane{}
	if err := util.UnmarshalWithJSONPB(string(values), icp, false); err == nil {
		log.Info("Input file has IstioControlPlane format.")
		translations, err := translate.ICPtoIOPTranslations(binversion.OperatorBinaryVersion)
		if err != nil {
			return err
		}
		out, err := translate.ICPToIOP(string(values), translations)
		if err != nil {
			return err
		}
		l.logAndPrint(out)
		return nil
	}

	// Not IstioControlPlane, try Helm values.yaml.
	mvs := binversion.OperatorBinaryVersion.MinorVersion
	ts, err := translate.NewReverseTranslator(mvs)
	if err != nil {
		return fmt.Errorf("error creating values.yaml translator: %s", err)
	}

	// verify the input schema first
	if errs := validate.CheckValuesString(values); len(errs) != 0 {
		return validate.GenValidateError(mvs, errs.ToError())
	}

	translatedIOPS, err := ts.TranslateFromValueToSpec(values, force)
	if err != nil {
		return fmt.Errorf("error translating values.yaml: %s", err)
	}

	isCP := &iopv1alpha1.IstioOperator{Spec: translatedIOPS, Kind: "IstioOperator", ApiVersion: "install.istio.io/v1alpha1"}

	ms := jsonpb.Marshaler{}
	gotString, err := ms.MarshalToString(isCP)
	if err != nil {
		return fmt.Errorf("error marshaling translated IstioOperator: %s", err)
	}

	isCPYaml, err := yaml.JSONToYAML([]byte(gotString))
	if err != nil {
		return fmt.Errorf("error converting JSON: %s\n%s", gotString, err)
	}

	l.print(string(isCPYaml) + "\n")
	return nil
}

// migrateFromClusterConfig handles migration for in cluster config.
func migrateFromClusterConfig(rootArgs *rootArgs, mmArgs *manifestMigrateArgs, l *Logger) error {
	initLogsOrExit(rootArgs)

	l.logAndPrint("translating in cluster specs\n")

	c := kubectlcmd.New()
	opts := &kubectlcmd.Options{
		Namespace: mmArgs.namespace,
		ExtraArgs: []string{"jsonpath='{.data.values}'"},
	}
	output, stderr, err := c.GetConfigMap("istio-sidecar-injector", opts)
	if err != nil {
		return err
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
		return fmt.Errorf("error unmarshaling JSON to untyped map %s", err)
	}
	res, err := yaml.Marshal(value)
	if err != nil {
		return fmt.Errorf("error marshaling untyped map to YAML: %s", err)
	}

	return translateFunc(res, mmArgs.force, l)
}
