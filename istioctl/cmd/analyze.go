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
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/processing/snapshotter"

	"github.com/ghodss/yaml"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"istio.io/pkg/env"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/analyzers"
	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/analysis/local"
	cfgKube "istio.io/istio/galley/pkg/config/source/kube"
	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/kube"
)

// AnalyzerFoundIssuesError indicates that at least one analyzer found problems.
type AnalyzerFoundIssuesError struct{}

// FileParseError indicates a provided file was unable to be parsed.
type FileParseError struct{}

const (
	FileParseString = "Some files couldn't be parsed."
	LogOutput       = "log"
	JSONOutput      = "json"
	YamlOutput      = "yaml"
)

func (f AnalyzerFoundIssuesError) Error() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Analyzers found issues when analyzing %s.\n", analyzeTargetAsString()))
	sb.WriteString(fmt.Sprintf("See %s for more information about causes and resolutions.", diag.DocPrefix))
	return sb.String()
}

func (f FileParseError) Error() string {
	return FileParseString
}

var (
	listAnalyzers     bool
	useKube           bool
	failureLevel      = messageThreshold{diag.Warning} // messages at least this level will generate an error exit code
	outputLevel       = messageThreshold{diag.Info}    // messages at least this level will be included in the output
	colorize          bool
	msgOutputFormat   string
	meshCfgFile       string
	selectedNamespace string
	allNamespaces     bool
	suppress          []string
	analysisTimeout   time.Duration
	recursive         bool

	termEnvVar = env.RegisterStringVar("TERM", "", "Specifies terminal type.  Use 'dumb' to suppress color output")

	colorPrefixes = map[diag.Level]string{
		diag.Info:    "",           // no special color for info messages
		diag.Warning: "\033[33m",   // yellow
		diag.Error:   "\033[1;31m", // bold red
	}

	fileExtensions = []string{".json", ".yaml", ".yml"}
)

// Analyze command
func Analyze() *cobra.Command {
	// Validate the output format before doing potentially expensive work to fail earlier
	msgOutputFormats := map[string]bool{LogOutput: true, JSONOutput: true, YamlOutput: true}
	var msgOutputFormatKeys []string

	for k := range msgOutputFormats {
		msgOutputFormatKeys = append(msgOutputFormatKeys, k)
	}
	sort.Strings(msgOutputFormatKeys)

	analysisCmd := &cobra.Command{
		Use:   "analyze <file>...",
		Short: "Analyze Istio configuration and print validation messages",
		Example: `
# Analyze the current live cluster
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
istioctl analyze -L
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			msgOutputFormat = strings.ToLower(msgOutputFormat)
			_, ok := msgOutputFormats[msgOutputFormat]
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

			// If we've explicitly asked for all namespaces, blank the selectedNamespace var out
			if allNamespaces {
				selectedNamespace = ""
			}

			sa := local.NewSourceAnalyzer(schema.MustGet(), analyzers.AllCombined(),
				resource.Namespace(selectedNamespace), resource.Namespace(istioNamespace), nil, true, analysisTimeout)

			// Check for suppressions and add them to our SourceAnalyzer
			var suppressions []snapshotter.AnalysisSuppression
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
				suppressions = append(suppressions, snapshotter.AnalysisSuppression{
					Code:         parts[0],
					ResourceName: parts[1],
				})
			}
			sa.SetSuppressions(suppressions)

			// If we're using kube, use that as a base source.
			if useKube {
				// Set up the kube client
				config := kube.BuildClientCmd(kubeconfig, configContext)
				restConfig, err := config.ClientConfig()
				if err != nil {
					return err
				}
				k := cfgKube.NewInterfaces(restConfig)
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

			// Filter outputMessages by specified level, and append a ref arg to the doc URL
			var outputMessages diag.Messages
			for _, m := range result.Messages {
				if m.Type.Level().IsWorseThanOrEqualTo(outputLevel.Level) {
					m.DocRef = "istioctl-analyze"
					outputMessages = append(outputMessages, m)
				}
			}

			switch msgOutputFormat {
			case LogOutput:
				// Print validation message output, or a line indicating that none were found
				if len(outputMessages) == 0 {
					if parseErrors == 0 {
						fmt.Fprintf(cmd.ErrOrStderr(), "\u2714 No validation issues found when analyzing %s.\n", analyzeTargetAsString())
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
				} else {
					for _, m := range outputMessages {
						fmt.Fprintln(cmd.OutOrStdout(), renderMessage(m))
					}
				}
			case JSONOutput:
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

	analysisCmd.PersistentFlags().BoolVarP(&listAnalyzers, "list-analyzers", "L", false,
		"List the analyzers available to run. Suppresses normal execution.")
	analysisCmd.PersistentFlags().BoolVarP(&useKube, "use-kube", "k", true,
		"Use live Kubernetes cluster for analysis. Set --use-kube=false to analyze files only.")
	analysisCmd.PersistentFlags().BoolVar(&colorize, "color", istioctlColorDefault(analysisCmd),
		"Default true.  Disable with '=false' or set $TERM to dumb")
	analysisCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false,
		"Enable verbose output")
	analysisCmd.PersistentFlags().Var(&failureLevel, "failure-threshold",
		fmt.Sprintf("The severity level of analysis at which to set a non-zero exit code. Valid values: %v", diag.GetAllLevelStrings()))
	analysisCmd.PersistentFlags().Var(&outputLevel, "output-threshold",
		fmt.Sprintf("The severity level of analysis at which to display messages. Valid values: %v", diag.GetAllLevelStrings()))
	analysisCmd.PersistentFlags().StringVarP(&msgOutputFormat, "output", "o", LogOutput,
		fmt.Sprintf("Output format: one of %v", msgOutputFormatKeys))
	analysisCmd.PersistentFlags().StringVar(&meshCfgFile, "meshConfigFile", "",
		"Overrides the mesh config values to use for analysis.")
	analysisCmd.PersistentFlags().BoolVarP(&allNamespaces, "all-namespaces", "A", false,
		"Analyze all namespaces")
	analysisCmd.PersistentFlags().StringArrayVarP(&suppress, "suppress", "S", []string{},
		"Suppress reporting a message code on a specific resource. Values are supplied in the form "+
			`<code>=<resource> (e.g. '--suppress "IST0102=DestinationRule primary-dr.default"'). Can be repeated. `+
			`You can include the wildcard character '*' to support a partial match (e.g. '--suppress "IST0102=DestinationRule *.default" ).`)
	analysisCmd.PersistentFlags().DurationVar(&analysisTimeout, "timeout", 30*time.Second,
		"the duration to wait before failing")
	analysisCmd.PersistentFlags().BoolVarP(&recursive, "recursive", "R", false,
		"Process directory arguments recursively. Useful when you want to analyze related manifests organized within the same directory.")
	return analysisCmd
}

func gatherFiles(cmd *cobra.Command, args []string) ([]local.ReaderSource, error) {
	var readers []local.ReaderSource
	for _, f := range args {
		var r *os.File

		// Handle "-" as stdin as a special case.
		if f == "-" {
			if isatty.IsTerminal(os.Stdin.Fd()) {
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
				fmt.Fprintf(cmd.OutOrStderr(), "Skipping file %v, recognized file extensions are: %v\n", f, fileExtensions)
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
			fmt.Fprintf(cmd.OutOrStdout(), "Skipping file %v, recognized file extensions are: %v\n", path, fileExtensions)
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
	if m.Resource != nil {
		loc := ""
		if m.Resource.Origin.Reference() != nil {
			loc = " " + m.Resource.Origin.Reference().String()
		}
		origin = " (" + m.Resource.Origin.FriendlyName() + loc + ")"
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

func isValidFile(f string) bool {
	ext := filepath.Ext(f)
	for _, e := range fileExtensions {
		if e == ext {
			return true
		}
	}
	return false
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
