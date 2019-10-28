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
	"fmt"
	"os"
	"strings"

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

const (
	FoundIssueString = "Analyzer found issues."
)

func (f AnalyzerFoundIssuesError) Error() string {
	return FoundIssueString
}

var (
	useKube               bool
	useDiscovery          string
	messageLevelThreshold = diag.Warning // messages at least this level will generate an error exit code
	colorize              bool

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
			if len(files) > 0 {
				if err = sa.AddFileKubeSource(files); err != nil {
					// Partial success is possible, so don't return early, but do print.
					// TODO(https://github.com/istio/istio/issues/17862): If we had any such errors, we should return a nonzero exit code
					fmt.Fprintf(cmd.ErrOrStderr(), "Error(s) reading files: %v", err)
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

			if len(result.Messages) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "\u2714 No validation issues found.")
			} else {
				for _, m := range result.Messages {
					fmt.Fprintln(cmd.OutOrStdout(), renderMessage(m))
				}
			}

			return errorIfMessagesExceedThreshold(result.Messages)
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
		if m.Type.Level().IsWorseThanOrEqualTo(messageLevelThreshold) {
			foundIssues = true
		}
	}

	if foundIssues {
		return AnalyzerFoundIssuesError{}
	}

	return nil
}
