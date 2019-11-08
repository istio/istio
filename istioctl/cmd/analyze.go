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

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"istio.io/pkg/env"

	"istio.io/istio/galley/pkg/config/analysis/analyzers"
	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/analysis/local"
	"istio.io/istio/galley/pkg/config/meta/metadata"
	cfgKube "istio.io/istio/galley/pkg/config/source/kube"
	"istio.io/istio/pkg/kube"
)

type AnalyzerFoundIssuesError struct{}
type FileParseError struct{}

const (
	FoundIssueString = "Analyzer found issues."
	FileParseString  = "Some files couldn't be parsed."
	LogOutput        = "log"
	JsonOutput       = "json"
	YamlOutput       = "yaml"
)

func (f AnalyzerFoundIssuesError) Error() string {
	return FoundIssueString
}

func (f FileParseError) Error() string {
	return FileParseString
}

var (
	useKube         bool
	useDiscovery    string
	failureLevel    = messageThreshold{diag.Warning} // messages at least this level will generate an error exit code
	outputLevel     = messageThreshold{diag.Info}    // messages at least this level will be included in the output
	colorize        bool
	msgOutputFormat string

	termEnvVar = env.RegisterStringVar("TERM", "", "Specifies terminal type.  Use 'dumb' to suppress color output")

	colorPrefixes = map[diag.Level]string{
		diag.Info:    "",           // no special color for info messages
		diag.Warning: "\033[33m",   // yellow
		diag.Error:   "\033[1;31m", // bold red
	}
)

// Analyze command
// Once we're ready to move this functionality out of the "experimental" subtree, we should merge
// with `istioctl validate`. https://github.com/istio/istio/issues/16777
func Analyze() *cobra.Command {
	// Validate the output format before doing potentially expensive work to fail earlier
	msgOutputFormats := map[string]bool{LogOutput: true, JsonOutput: true, YamlOutput: true}
	var msgOutputFormatKeys []string

	for k, _ := range msgOutputFormats {
		msgOutputFormatKeys = append(msgOutputFormatKeys, k)
	}

	analysisCmd := &cobra.Command{
		Use:   "analyze <file>...",
		Short: "Analyze Istio configuration and print validation messages",
		Example: `
# Analyze yaml files
istioctl experimental analyze a.yaml b.yaml

# Analyze the current live cluster
istioctl experimental analyze -k

# Analyze the current live cluster, simulating the effect of applying additional yaml files
istioctl experimental analyze -k a.yaml b.yaml

# Analyze yaml files, overriding service discovery to enabled
istioctl experimental analyze -d true a.yaml b.yaml services.yaml

# Analyze the current live cluster, overriding service discovery to disabled
istioctl experimental analyze -k -d false
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			msgOutputFormat = strings.ToLower(msgOutputFormat)
			_, ok := msgOutputFormats[msgOutputFormat]
			if !ok {
				return CommandParseError{
					fmt.Errorf("%s not a valid option for format. See istioctl x analyze --help", msgOutputFormat),
				}
			}

			files, err := gatherFiles(args)
			if err != nil {
				return err
			}
			cancel := make(chan struct{})

			sd, err := serviceDiscovery()
			if err != nil {
				return err
			}

			// We use the "namespace" arg that's provided as part of root istioctl as a flag for specifying what namespace to use
			// for file resources that don't have one specified.
			// Note that the current implementation (in root.go) doesn't correctly default this value based on --context, so we do that ourselves
			// below since for the time being we want to keep changes isolated to experimental code. When we merge this into
			// istioctl validate (see https://github.com/istio/istio/issues/16777) we should look into fixing getDefaultNamespace in root
			// so it properly handles the --context option.
			selectedNamespace := namespace

			var k cfgKube.Interfaces
			if useKube {
				// Set up the kube client
				config := kube.BuildClientCmd(kubeconfig, configContext)
				restConfig, err := config.ClientConfig()
				if err != nil {
					return err
				}
				k = cfgKube.NewInterfaces(restConfig)

				// If a default namespace to inject in files hasn't been explicitly defined already, use whatever is specified in the kube context
				if selectedNamespace == "" {
					ns, _, err := config.Namespace()
					if err != nil {
						return err
					}
					selectedNamespace = ns
				}
			}

			// If default namespace to inject wasn't specified by the user or derived from the k8s context, just use the default.
			if selectedNamespace == "" {
				selectedNamespace = defaultNamespace
			}

			sa := local.NewSourceAnalyzer(metadata.MustGet(), analyzers.AllCombined(), selectedNamespace, nil, sd)

			// If we're using kube, use that as a base source.
			if k != nil {
				sa.AddRunningKubeSource(k)
			}

			// If files are provided, treat them (collectively) as a source.
			parseErrors := 0
			if len(files) > 0 {
				if err = sa.AddFileKubeSource(files); err != nil {
					// Partial success is possible, so don't return early, but do print.
					fmt.Fprintf(cmd.ErrOrStderr(), "Error(s) reading files: %v", err)
					parseErrors++
				}
			}

			result, err := sa.Analyze(cancel)
			if err != nil {
				return err
			}

			// Maybe output details about which analyzers ran
			if verbose {
				if len(result.SkippedAnalyzers) > 0 {
					fmt.Fprintln(cmd.ErrOrStderr(), "Skipped analyzers:")
					for _, a := range result.SkippedAnalyzers {
						fmt.Fprintln(cmd.ErrOrStderr(), "\t", a)
					}
				}
				if len(result.ExecutedAnalyzers) > 0 {
					fmt.Fprintln(cmd.ErrOrStderr(), "Executed analyzers:")
					for _, a := range result.ExecutedAnalyzers {
						fmt.Fprintln(cmd.ErrOrStderr(), "\t", a)
					}
				}
				fmt.Fprintln(cmd.ErrOrStderr())
			}

			// Filter outputMessages by specified level
			var outputMessages diag.Messages
			for _, m := range result.Messages {
				if m.Type.Level().IsWorseThanOrEqualTo(outputLevel.Level) {
					outputMessages = append(outputMessages, m)
				}
			}

			switch msgOutputFormat {
			case LogOutput:
				// Print validation message output, or a line indicating that none were found
				if len(outputMessages) == 0 {
					if parseErrors == 0 {
						fmt.Fprintln(cmd.ErrOrStderr(), "\u2714 No validation issues found.")
					} else {
						fileOrFiles := "files"
						if parseErrors == 1 {
							fileOrFiles = "file"
						}
						fmt.Fprintf(cmd.ErrOrStderr(),
							"No validation issues found (but %d %s could not be parsed)\n",
							parseErrors,
							fileOrFiles,
						)
					}
				} else {
					for _, m := range outputMessages {
						fmt.Fprintln(cmd.OutOrStdout(), renderMessage(m))
					}
				}
			case JsonOutput:
				jsonOutput, err := json.MarshalIndent(outputMessages, "", "\t")
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(jsonOutput))
			case YamlOutput:
				yamlOutput, err := yaml.Marshal(outputMessages)
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(yamlOutput))
			default: // This should never happen since we validate this already
				panic(fmt.Sprintf("%q not found in output format switch statement post validate?", msgOutputFormat))
			}

			// Return code is based on the unfiltered validation message list/parse errors
			// We're intentionally keeping failure threshold and output threshold decoupled for now
			returnError := errorIfMessagesExceedThreshold(result.Messages)
			if returnError == nil && parseErrors > 0 {
				returnError = FileParseError{}
			}
			return returnError
		},
	}

	analysisCmd.PersistentFlags().BoolVarP(&useKube, "use-kube", "k", false,
		"Use live Kubernetes cluster for analysis")
	analysisCmd.PersistentFlags().StringVarP(&useDiscovery, "discovery", "d", "",
		"'true' to enable service discovery, 'false' to disable it. "+
			"Defaults to true if --use-kube is set, false otherwise. "+
			"Analyzers requiring resources made available by enabling service discovery will be skipped.")
	analysisCmd.PersistentFlags().BoolVar(&colorize, "color", istioctlColorDefault(analysisCmd),
		"Default true.  Disable with '=false' or set $TERM to dumb")
	analysisCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
	analysisCmd.PersistentFlags().Var(&failureLevel, "failure-threshold",
		fmt.Sprintf("The severity level of analysis at which to set a non-zero exit code. Valid values: %v", diag.GetAllLevelStrings()))
	analysisCmd.PersistentFlags().Var(&outputLevel, "output-threshold",
		fmt.Sprintf("The severity level of analysis at which to display messages. Valid values: %v", diag.GetAllLevelStrings()))
	analysisCmd.PersistentFlags().StringVarP(&msgOutputFormat, "output", "o", LogOutput, fmt.Sprintf("Output format: one of %v", msgOutputFormatKeys))
	return analysisCmd
}

func gatherFiles(args []string) ([]string, error) {
	var result []string
	for _, a := range args {
		if _, err := os.Stat(a); err != nil {
			return nil, fmt.Errorf("could not find file %q", a)
		}
		result = append(result, a)
	}
	return result, nil
}

func serviceDiscovery() (bool, error) {
	switch strings.ToLower(useDiscovery) {
	case "":
		return useKube, nil
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("invalid argument value for discovery")
	}
}

func colorPrefix(m diag.Message) string {
	if !colorize {
		return ""
	}

	prefix, ok := colorPrefixes[m.Type.Level()]
	if !ok {
		return ""
	}

	return prefix
}

func colorSuffix() string {
	if !colorize {
		return ""
	}

	return "\033[0m"
}

func renderMessage(m diag.Message) string {
	origin := ""
	if m.Origin != nil {
		origin = " (" + m.Origin.FriendlyName() + ")"
	}
	return fmt.Sprintf(
		"%s%v%s [%v]%s %s", colorPrefix(m), m.Type.Level(), colorSuffix(), m.Type.Code(), origin, fmt.Sprintf(m.Type.Template(), m.Parameters...))
}

func istioctlColorDefault(cmd *cobra.Command) bool {
	if strings.EqualFold(termEnvVar.Get(), "dumb") {
		return false
	}

	file, ok := cmd.OutOrStdout().(*os.File)
	if ok {
		if !isatty.IsTerminal(file.Fd()) {
			return false
		}
	}

	return true
}

func errorIfMessagesExceedThreshold(messages []diag.Message) error {
	foundIssues := false
	for _, m := range messages {
		if m.Type.Level().IsWorseThanOrEqualTo(failureLevel.Level) {
			foundIssues = true
		}
	}

	if foundIssues {
		return AnalyzerFoundIssuesError{}
	}

	return nil
}

type messageThreshold struct {
	diag.Level
}

// String satisfies interface pflag.Value
func (m *messageThreshold) String() string {
	return m.Level.String()
}

// Type satisfies interface pflag.Value
func (m *messageThreshold) Type() string {
	return "Level"
}

// Set satisfies interface pflag.Value
func (m *messageThreshold) Set(s string) error {
	l, err := LevelFromString(s)
	if err != nil {
		return err
	}
	m.Level = l
	return nil
}

func LevelFromString(s string) (diag.Level, error) {
	val, ok := diag.GetUppercaseStringToLevelMap()[strings.ToUpper(s)]
	if !ok {
		return diag.Level{}, fmt.Errorf("%q not a valid option, please choose from: %v", s, diag.GetAllLevelStrings())
	}

	return val, nil
}
