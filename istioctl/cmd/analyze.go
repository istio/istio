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

package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/istioctl/pkg/util/formatting"
	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers"
	"istio.io/istio/pkg/config/analysis/diag"
	"istio.io/istio/pkg/config/analysis/local"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/url"
)

// AnalyzerFoundIssuesError indicates that at least one analyzer found problems.
type AnalyzerFoundIssuesError struct{}

// FileParseError indicates a provided file was unable to be parsed.
type FileParseError struct{}

const FileParseString = "Some files couldn't be parsed."

func (f AnalyzerFoundIssuesError) Error() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Analyzers found issues when analyzing %s.\n", analyzeTargetAsString()))
	sb.WriteString(fmt.Sprintf("See %s for more information about causes and resolutions.", url.ConfigAnalysis))
	return sb.String()
}

func (f FileParseError) Error() string {
	return FileParseString
}

var (
	listAnalyzers     bool
	useKube           bool
	failureThreshold  = formatting.MessageThreshold{diag.Error} // messages at least this level will generate an error exit code
	outputThreshold   = formatting.MessageThreshold{diag.Info}  // messages at least this level will be included in the output
	colorize          bool
	msgOutputFormat   string
	meshCfgFile       string
	selectedNamespace string
	allNamespaces     bool
	suppress          []string
	analysisTimeout   time.Duration
	recursive         bool
	ignoreUnknown     bool

	fileExtensions = []string{".json", ".yaml", ".yml"}
)

// Analyze command
func Analyze() *cobra.Command {
	analysisCmd := &cobra.Command{
		Use:   "analyze <file>...",
		Short: "Analyze Istio configuration and print validation messages",
		Example: `  # Analyze the current live cluster
  istioctl analyze

  # Analyze the current live cluster, simulating the effect of applying additional yaml files
  istioctl analyze a.yaml b.yaml my-app-config/

  # Analyze the current live cluster, simulating the effect of applying a directory of config recursively
  istioctl analyze --recursive my-istio-config/

  # Analyze yaml files without connecting to a live cluster
  istioctl analyze --use-kube=false a.yaml b.yaml my-app-config/

  # Analyze the current live cluster and suppress PodMissingProxy for pod mypod in namespace 'testing'.
  istioctl analyze -S "IST0103=Pod mypod.testing"

  # Analyze the current live cluster and suppress PodMissingProxy for all pods in namespace 'testing',
  # and suppress MisplacedAnnotation on deployment foobar in namespace default.
  istioctl analyze -S "IST0103=Pod *.testing" -S "IST0107=Deployment foobar.default"

  # List available analyzers
  istioctl analyze -L`,
		RunE: func(cmd *cobra.Command, args []string) error {
			msgOutputFormat = strings.ToLower(msgOutputFormat)
			_, ok := formatting.MsgOutputFormats[msgOutputFormat]
			if !ok {
				return CommandParseError{
					fmt.Errorf("%s not a valid option for format. See istioctl analyze --help", msgOutputFormat),
				}
			}

			if listAnalyzers {
				fmt.Print(AnalyzersAsString(analyzers.All()))
				return nil
			}

			readers, err := gatherFiles(cmd, args)
			if err != nil {
				return err
			}
			cancel := make(chan struct{})

			// We use the "namespace" arg that's provided as part of root istioctl as a flag for specifying what namespace to use
			// for file resources that don't have one specified.
			selectedNamespace = handlers.HandleNamespace(namespace, defaultNamespace)

			// check whether selected namespace exists.
			if namespace != "" && useKube {
				client, err := kube.NewExtendedClient(kube.BuildClientCmd(kubeconfig, configContext), "")
				if err != nil {
					return err
				}
				_, err = client.Kube().CoreV1().Namespaces().Get(context.TODO(), namespace, v1.GetOptions{})
				if errors.IsNotFound(err) {
					fmt.Fprintf(cmd.ErrOrStderr(), "namespace %q not found\n", namespace)
					return nil
				}
			}

			// If we've explicitly asked for all namespaces, blank the selectedNamespace var out
			if allNamespaces {
				selectedNamespace = ""
			}

			sa := local.NewIstiodAnalyzer(analyzers.AllCombined(),
				resource.Namespace(selectedNamespace),
				resource.Namespace(istioNamespace), nil, true)

			// Check for suppressions and add them to our SourceAnalyzer
			suppressions := make([]local.AnalysisSuppression, 0, len(suppress))
			for _, s := range suppress {
				parts := strings.Split(s, "=")
				if len(parts) != 2 {
					return fmt.Errorf("%s is not a valid suppression value. See istioctl analyze --help", s)
				}
				// Check to see if the supplied code is valid. If not, emit a
				// warning but continue.
				codeIsValid := false
				for _, at := range msg.All() {
					if at.Code() == parts[0] {
						codeIsValid = true
						break
					}
				}

				if !codeIsValid {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: Supplied message code '%s' is an unknown message code and will not have any effect.\n", parts[0])
				}
				suppressions = append(suppressions, local.AnalysisSuppression{
					Code:         parts[0],
					ResourceName: parts[1],
				})
			}
			sa.SetSuppressions(suppressions)

			// If we're using kube, use that as a base source.
			if useKube {
				// Set up the kube client
				restConfig, err := kube.DefaultRestConfig(kubeconfig, configContext)
				if err != nil {
					return err
				}
				k, err := kube.NewClient(kube.NewClientConfigForRestConfig(restConfig))
				if err != nil {
					return err
				}
				sa.AddRunningKubeSource(k)
			}

			// If we explicitly specify mesh config, use it.
			// This takes precedence over default mesh config or mesh config from a running Kube instance.
			if meshCfgFile != "" {
				_ = sa.AddFileKubeMeshConfig(meshCfgFile)
			}

			// If we're not using kube (files only), add defaults for some resources we expect to be provided by Istio
			if !useKube {
				err := sa.AddDefaultResources()
				if err != nil {
					return err
				}
			}

			// If files are provided, treat them (collectively) as a source.
			parseErrors := 0
			if len(readers) > 0 {
				if err = sa.AddReaderKubeSource(readers); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Error(s) adding files: %v", err)
					parseErrors++
				}
			}

			// Do the analysis
			result, err := sa.Analyze(cancel)
			if err != nil {
				return err
			}

			// Maybe output details about which analyzers ran
			if verbose {
				fmt.Fprintf(cmd.ErrOrStderr(), "Analyzed resources in %s\n", analyzeTargetAsString())

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

			// Get messages for output
			outputMessages := result.Messages.SetDocRef("istioctl-analyze").FilterOutLowerThan(outputThreshold.Level)

			// Print all the messages to stdout in the specified format
			output, err := formatting.Print(outputMessages, msgOutputFormat, colorize)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), output)

			// An extra message on success
			if len(outputMessages) == 0 {
				if parseErrors == 0 {
					if len(readers) > 0 {
						var files []string
						for _, r := range readers {
							files = append(files, r.Name)
						}
						fmt.Fprintf(cmd.ErrOrStderr(), "\u2714 No validation issues found when analyzing %s.\n", strings.Join(files, "\n"))
					} else {
						fmt.Fprintf(cmd.ErrOrStderr(), "\u2714 No validation issues found when analyzing %s.\n", analyzeTargetAsString())
					}
				} else {
					fileOrFiles := "files"
					if parseErrors == 1 {
						fileOrFiles = "file"
					}
					fmt.Fprintf(cmd.ErrOrStderr(),
						"No validation issues found when analyzing %s (but %d %s could not be parsed).\n",
						analyzeTargetAsString(),
						parseErrors,
						fileOrFiles,
					)
				}
			}

			// Return code is based on the unfiltered validation message list/parse errors
			// We're intentionally keeping failure threshold and output threshold decoupled for now
			var returnError error
			if msgOutputFormat == formatting.LogFormat {
				returnError = errorIfMessagesExceedThreshold(result.Messages)
				if returnError == nil && parseErrors > 0 && !ignoreUnknown {
					returnError = FileParseError{}
				}
			}
			return returnError
		},
	}

	analysisCmd.PersistentFlags().BoolVarP(&listAnalyzers, "list-analyzers", "L", false,
		"List the analyzers available to run. Suppresses normal execution.")
	analysisCmd.PersistentFlags().BoolVarP(&useKube, "use-kube", "k", true,
		"Use live Kubernetes cluster for analysis. Set --use-kube=false to analyze files only.")
	analysisCmd.PersistentFlags().BoolVar(&colorize, "color", formatting.IstioctlColorDefault(analysisCmd.OutOrStdout()),
		"Default true.  Disable with '=false' or set $TERM to dumb")
	analysisCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false,
		"Enable verbose output")
	analysisCmd.PersistentFlags().Var(&failureThreshold, "failure-threshold",
		fmt.Sprintf("The severity level of analysis at which to set a non-zero exit code. Valid values: %v", diag.GetAllLevelStrings()))
	analysisCmd.PersistentFlags().Var(&outputThreshold, "output-threshold",
		fmt.Sprintf("The severity level of analysis at which to display messages. Valid values: %v", diag.GetAllLevelStrings()))
	analysisCmd.PersistentFlags().StringVarP(&msgOutputFormat, "output", "o", formatting.LogFormat,
		fmt.Sprintf("Output format: one of %v", formatting.MsgOutputFormatKeys))
	analysisCmd.PersistentFlags().StringVar(&meshCfgFile, "meshConfigFile", "",
		"Overrides the mesh config values to use for analysis.")
	analysisCmd.PersistentFlags().BoolVarP(&allNamespaces, "all-namespaces", "A", false,
		"Analyze all namespaces")
	analysisCmd.PersistentFlags().StringArrayVarP(&suppress, "suppress", "S", []string{},
		"Suppress reporting a message code on a specific resource. Values are supplied in the form "+
			`<code>=<resource> (e.g. '--suppress "IST0102=DestinationRule primary-dr.default"'). Can be repeated. `+
			`You can include the wildcard character '*' to support a partial match (e.g. '--suppress "IST0102=DestinationRule *.default" ).`)
	analysisCmd.PersistentFlags().DurationVar(&analysisTimeout, "timeout", 30*time.Second,
		"The duration to wait before failing")
	analysisCmd.PersistentFlags().BoolVarP(&recursive, "recursive", "R", false,
		"Process directory arguments recursively. Useful when you want to analyze related manifests organized within the same directory.")
	analysisCmd.PersistentFlags().BoolVar(&ignoreUnknown, "ignore-unknown", false,
		"Don't complain about un-parseable input documents, for cases where analyze should run only on k8s compliant inputs.")
	return analysisCmd
}

func gatherFiles(cmd *cobra.Command, args []string) ([]local.ReaderSource, error) {
	var readers []local.ReaderSource
	for _, f := range args {
		var r *os.File

		// Handle "-" as stdin as a special case.
		if f == "-" {
			if isatty.IsTerminal(os.Stdin.Fd()) && !isJSONorYAMLOutputFormat() {
				fmt.Fprint(cmd.OutOrStdout(), "Reading from stdin:\n")
			}
			r = os.Stdin
			readers = append(readers, local.ReaderSource{Name: f, Reader: r})
			continue
		}

		fi, err := os.Stat(f)
		if err != nil {
			return nil, err
		}

		if fi.IsDir() {
			dirReaders, err := gatherFilesInDirectory(cmd, f)
			if err != nil {
				return nil, err
			}
			readers = append(readers, dirReaders...)
		} else {
			if !isValidFile(f) {
				fmt.Fprintf(cmd.ErrOrStderr(), "Skipping file %v, recognized file extensions are: %v\n", f, fileExtensions)
				continue
			}
			rs, err := gatherFile(f)
			if err != nil {
				return nil, err
			}
			readers = append(readers, rs)
		}
	}
	return readers, nil
}

func gatherFile(f string) (local.ReaderSource, error) {
	r, err := os.Open(f)
	if err != nil {
		return local.ReaderSource{}, err
	}
	runtime.SetFinalizer(r, func(x *os.File) { x.Close() })
	return local.ReaderSource{Name: f, Reader: r}, nil
}

func gatherFilesInDirectory(cmd *cobra.Command, dir string) ([]local.ReaderSource, error) {
	var readers []local.ReaderSource

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// If we encounter a directory, recurse only if the --recursve option
		// was provided and the directory is not the same as dir.
		if info.IsDir() {
			if !recursive && dir != path {
				return filepath.SkipDir
			}
			return nil
		}

		if !isValidFile(path) {
			fmt.Fprintf(cmd.ErrOrStderr(), "Skipping file %v, recognized file extensions are: %v\n", path, fileExtensions)
			return nil
		}

		r, err := os.Open(path)
		if err != nil {
			return err
		}
		runtime.SetFinalizer(r, func(x *os.File) { x.Close() })
		readers = append(readers, local.ReaderSource{Name: path, Reader: r})
		return nil
	})
	return readers, err
}

func errorIfMessagesExceedThreshold(messages []diag.Message) error {
	foundIssues := false
	for _, m := range messages {
		if m.Type.Level().IsWorseThanOrEqualTo(failureThreshold.Level) {
			foundIssues = true
		}
	}

	if foundIssues {
		return AnalyzerFoundIssuesError{}
	}

	return nil
}

func isValidFile(f string) bool {
	ext := filepath.Ext(f)
	for _, e := range fileExtensions {
		if e == ext {
			return true
		}
	}
	return false
}

func AnalyzersAsString(analyzers []analysis.Analyzer) string {
	nameToAnalyzer := make(map[string]analysis.Analyzer)
	analyzerNames := make([]string, len(analyzers))
	for i, a := range analyzers {
		analyzerNames[i] = a.Metadata().Name
		nameToAnalyzer[a.Metadata().Name] = a
	}
	sort.Strings(analyzerNames)

	var b strings.Builder
	for _, aName := range analyzerNames {
		b.WriteString(fmt.Sprintf("* %s:\n", aName))
		a := nameToAnalyzer[aName]
		if a.Metadata().Description != "" {
			b.WriteString(fmt.Sprintf("    %s\n", a.Metadata().Description))
		}
	}
	return b.String()
}

func analyzeTargetAsString() string {
	if allNamespaces {
		return "all namespaces"
	}
	return fmt.Sprintf("namespace: %s", selectedNamespace)
}

// TODO: Refactor output writer so that it is smart enough to know when to output what.
func isJSONorYAMLOutputFormat() bool {
	return msgOutputFormat == formatting.JSONFormat || msgOutputFormat == formatting.YAMLFormat
}
