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

	"github.com/ghodss/yaml"

	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/translate"
	"istio.io/istio/operator/pkg/util"

	"github.com/spf13/cobra"
)

type profileDumpArgs struct {
	// inFilenames is an array of paths to the input IstioOperator CR files.
	inFilenames []string
	// configPath sets the root node for the subtree to display the config for.
	configPath string
	// outputFormat controls the format of profile dumps
	outputFormat string
}

const (
	jsonOutput = "json"
	yamlOutput = "yaml"
)

func addProfileDumpFlags(cmd *cobra.Command, args *profileDumpArgs) {
	cmd.PersistentFlags().StringSliceVarP(&args.inFilenames, "filename", "f", nil, filenameFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.configPath, "config-path", "p", "",
		"The path the root of the configuration subtree to dump e.g. trafficManagement.components.pilot. By default, dump whole tree")
	cmd.PersistentFlags().StringVarP(&args.outputFormat, "output", "o", yamlOutput,
		"Output format: one of json|yaml")
}

func profileDumpCmd(rootArgs *rootArgs, pdArgs *profileDumpArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "dump [<profile>]",
		Short: "Dumps an Istio configuration profile",
		Long:  "The dump subcommand dumps the values in an Istio configuration profile.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return fmt.Errorf("too many positional arguments")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			l := NewLogger(rootArgs.logToStdErr, cmd.OutOrStdout(), cmd.ErrOrStderr())
			return profileDump(args, rootArgs, pdArgs, l)
		}}

}

func prependHeader(yml string) (string, error) {
	out, err := tpath.AddSpecRoot(yml)
	if err != nil {
		return "", err
	}
	out2, err := util.OverlayYAML(translate.IstioOperatorTreeString, out)
	if err != nil {
		return "", err
	}
	return out2, nil
}

// Convert the generated YAML to pretty JSON.
func yamlToPrettyJSON(yml string) (string, error) {
	// YAML objects are not completely compatible with JSON
	// objects. Let yaml.YAMLToJSON handle the edge cases and
	// we'll re-encode the result to pretty JSON.
	uglyJSON, err := yaml.YAMLToJSON([]byte(yml))
	if err != nil {
		return "", err
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(uglyJSON, &decoded); err != nil {
		return "", err
	}
	prettyJSON, err := json.MarshalIndent(decoded, "", "    ")
	if err != nil {
		return "", err
	}
	return string(prettyJSON), nil
}

func profileDump(args []string, rootArgs *rootArgs, pdArgs *profileDumpArgs, l *Logger) error {
	initLogsOrExit(rootArgs)

	if len(args) == 1 && pdArgs.inFilenames != nil {
		return fmt.Errorf("cannot specify both profile name and filename flag")
	}

	switch pdArgs.outputFormat {
	case jsonOutput, yamlOutput:
	default:
		return fmt.Errorf("unknown output format: %v", pdArgs.outputFormat)
	}

	setFlagYAML := ""
	if len(args) == 1 {
		var err error
		if setFlagYAML, err = tpath.AddSpecRoot("profile: " + args[0]); err != nil {
			return err
		}
	}

	y, _, err := GenerateConfig(pdArgs.inFilenames, setFlagYAML, true, nil, l)
	if err != nil {
		return err
	}

	y, err = tpath.GetConfigSubtree(y, pdArgs.configPath)
	if err != nil {
		return err
	}

	if pdArgs.configPath == "" {
		if y, err = prependHeader(y); err != nil {
			return err
		}
	} else {
		if y, err = tpath.GetConfigSubtree(y, pdArgs.configPath); err != nil {
			return err
		}
	}

	switch pdArgs.outputFormat {
	case jsonOutput:
		j, err := yamlToPrettyJSON(y)
		if err != nil {
			return err
		}
		l.print(j + "\n")
	case yamlOutput:
		l.print(y + "\n")
	}

	return nil
}
