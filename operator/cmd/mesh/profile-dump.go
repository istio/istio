// Copyright Istio Authors
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
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/pkg/log"
)

type profileDumpArgs struct {
	// inFilenames is an array of paths to the input IstioOperator CR files.
	inFilenames []string
	// configPath sets the root node for the subtree to display the config for.
	configPath string
	// outputFormat controls the format of profile dumps
	outputFormat string
	// manifestsPath is a path to a charts and profiles directory in the local filesystem, or URL with a release tgz.
	manifestsPath string
}

const (
	jsonOutput  = "json"
	yamlOutput  = "yaml"
	flagsOutput = "flags"
)

const (
	istioOperatorTreeString = `
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
`
)

func addProfileDumpFlags(cmd *cobra.Command, args *profileDumpArgs) {
	cmd.PersistentFlags().StringSliceVarP(&args.inFilenames, "filename", "f", nil, filenameFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.configPath, "config-path", "p", "",
		"The path the root of the configuration subtree to dump e.g. components.pilot. By default, dump whole tree")
	cmd.PersistentFlags().StringVarP(&args.outputFormat, "output", "o", yamlOutput,
		"Output format: one of json|yaml|flags")
	cmd.PersistentFlags().StringVarP(&args.manifestsPath, "charts", "", "", ChartsDeprecatedStr)
	cmd.PersistentFlags().StringVarP(&args.manifestsPath, "manifests", "d", "", ManifestsFlagHelpStr)
}

func profileDumpCmd(rootArgs *RootArgs, pdArgs *profileDumpArgs, logOpts *log.Options) *cobra.Command {
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
			l := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.ErrOrStderr(), installerScope)
			return profileDump(args, rootArgs, pdArgs, l, logOpts)
		},
	}
}

func prependHeader(yml string) (string, error) {
	out, err := tpath.AddSpecRoot(yml)
	if err != nil {
		return "", err
	}
	out2, err := util.OverlayYAML(istioOperatorTreeString, out)
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
	var decoded any
	if uglyJSON[0] == '[' {
		decoded = make([]any, 0)
	} else {
		decoded = map[string]any{}
	}
	if err := json.Unmarshal(uglyJSON, &decoded); err != nil {
		return "", err
	}
	prettyJSON, err := json.MarshalIndent(decoded, "", "    ")
	if err != nil {
		return "", err
	}
	return string(prettyJSON), nil
}

func profileDump(args []string, rootArgs *RootArgs, pdArgs *profileDumpArgs, l clog.Logger, logOpts *log.Options) error {
	initLogsOrExit(rootArgs)

	if len(args) == 1 && pdArgs.inFilenames != nil {
		return fmt.Errorf("cannot specify both profile name and filename flag")
	}

	if err := validateProfileOutputFormatFlag(pdArgs.outputFormat); err != nil {
		return err
	}

	setFlags := applyFlagAliases(make([]string, 0), pdArgs.manifestsPath, "")
	if len(args) == 1 {
		setFlags = append(setFlags, "profile="+args[0])
	}

	if err := configLogs(logOpts); err != nil {
		return fmt.Errorf("could not configure logs: %s", err)
	}

	y, _, err := manifest.GenerateConfig(pdArgs.inFilenames, setFlags, true, nil, l)
	if err != nil {
		return err
	}
	y, err = tpath.GetConfigSubtree(y, "spec")
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

	var output string
	if output, err = yamlToFormat(y, pdArgs.outputFormat); err != nil {
		return err
	}
	l.Print(output)
	return nil
}

// validateOutputFormatFlag validates if the output format is valid.
func validateProfileOutputFormatFlag(outputFormat string) error {
	switch outputFormat {
	case jsonOutput, yamlOutput, flagsOutput:
	default:
		return fmt.Errorf("unknown output format: %s", outputFormat)
	}
	return nil
}

// yamlToFormat converts the generated yaml config to the expected format
func yamlToFormat(yaml, outputFormat string) (string, error) {
	var output string
	switch outputFormat {
	case jsonOutput:
		j, err := yamlToPrettyJSON(yaml)
		if err != nil {
			return "", err
		}
		output = fmt.Sprintf("%s\n", j)
	case yamlOutput:
		output = fmt.Sprintf("%s\n", yaml)
	case flagsOutput:
		f, err := yamlToFlags(yaml)
		if err != nil {
			return "", err
		}
		output = fmt.Sprintf("%s\n", strings.Join(f, "\n"))
	}
	return output, nil
}

// Convert the generated YAML to --set flags
func yamlToFlags(yml string) ([]string, error) {
	// YAML objects are not completely compatible with JSON
	// objects. Let yaml.YAMLToJSON handle the edge cases and
	// we'll re-encode the result to pretty JSON.
	uglyJSON, err := yaml.YAMLToJSON([]byte(yml))
	if err != nil {
		return []string{}, err
	}
	var decoded any
	if uglyJSON[0] == '[' {
		decoded = make([]any, 0)
	} else {
		decoded = map[string]any{}
	}
	if err := json.Unmarshal(uglyJSON, &decoded); err != nil {
		return []string{}, err
	}
	if d, ok := decoded.(map[string]any); ok {
		if v, ok := d["spec"]; ok {
			// Fall back to showing the entire spec.
			// (When --config-path is used there will be no spec to remove)
			decoded = v
		}
	}
	setflags, err := walk("", "", decoded)
	if err != nil {
		return []string{}, err
	}
	sort.Strings(setflags)
	return setflags, nil
}

func walk(path, separator string, obj any) ([]string, error) {
	switch v := obj.(type) {
	case map[string]any:
		accum := make([]string, 0)
		for key, vv := range v {
			childwalk, err := walk(fmt.Sprintf("%s%s%s", path, separator, pathComponent(key)), ".", vv)
			if err != nil {
				return accum, err
			}
			accum = append(accum, childwalk...)
		}
		return accum, nil
	case []any:
		accum := make([]string, 0)
		for idx, vv := range v {
			indexwalk, err := walk(fmt.Sprintf("%s[%d]", path, idx), ".", vv)
			if err != nil {
				return accum, err
			}
			accum = append(accum, indexwalk...)
		}
		return accum, nil
	case string:
		return []string{fmt.Sprintf("%s=%q", path, v)}, nil
	default:
		return []string{fmt.Sprintf("%s=%v", path, v)}, nil
	}
}

func pathComponent(component string) string {
	if !strings.Contains(component, util.PathSeparator) {
		return component
	}
	return strings.ReplaceAll(component, util.PathSeparator, util.EscapedPathSeparator)
}
