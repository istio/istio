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

package analyze

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/util"
	"istio.io/istio/istioctl/pkg/util/formatting"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers"
	"istio.io/istio/pkg/config/analysis/diag"
	"istio.io/istio/pkg/config/analysis/local"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/kube/multicluster"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/url"
	"istio.io/istio/pkg/util/sets"
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
	failureThreshold  = formatting.MessageThreshold{Level: diag.Error} // messages at least this level will generate an error exit code
	outputThreshold   = formatting.MessageThreshold{Level: diag.Info}  // messages at least this level will be included in the output
	colorize          bool
	msgOutputFormat   string
	meshCfgFile       string
	selectedNamespace string
	allNamespaces     bool
	suppress          []string
	analysisTimeout   time.Duration
	recursive         bool
	ignoreUnknown     bool
	revisionSpecified string
	remoteContexts    []string
	selectedAnalyzers []string
	excludeNamespaces []string

	fileExtensions = []string{".json", ".yaml", ".yml"}
)

// Analyze command
func Analyze(ctx cli.Context) *cobra.Command {
	var verbose bool
	analysisCmd := &cobra.Command{
		Use:   "analyze <file>...",
		Short: "Analyze Istio configuration and print validation messages",
		Long: fmt.Sprintf("Analyze Istio configuration and print validation messages.\n"+
			"For more information about message codes, refer to:\n%s", url.ConfigAnalysis),
		Example: `  # Analyze the current live cluster
  istioctl analyze

  # Analyze the current live cluster for a specific revision
  istioctl analyze --revision 1-16

  # Analyze the current live cluster, simulating the effect of applying additional yaml files
  istioctl analyze a.yaml b.yaml my-app-config/

  # Analyze yaml files without connecting to a live cluster
  istioctl analyze --use-kube=false a.yaml b.yaml my-app-config/

  # Analyze the current live cluster and suppress PodMissingProxy for pod mypod in namespace 'testing'.
  istioctl analyze -S "IST0103=Pod mypod.testing"

  # Analyze the current live cluster and suppress PodMissingProxy for all pods in namespace 'testing',
  # and suppress MisplacedAnnotation on deployment foobar in namespace default.
  istioctl analyze -S "IST0103=Pod *.testing" -S "IST0107=Deployment foobar.default"

  # List available analyzers
  istioctl analyze -L
  
  # Run specific analyzer
  istioctl analyze --analyzer "gateway.ConflictingGatewayAnalyzer"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			msgOutputFormat = strings.ToLower(msgOutputFormat)
			_, ok := formatting.MsgOutputFormats[msgOutputFormat]
			if !ok {
				return util.CommandParseError{
					Err: fmt.Errorf("%s not a valid option for format. See istioctl analyze --help", msgOutputFormat),
				}
			}

			if listAnalyzers {
				fmt.Print(AnalyzersAsString(analyzers.All()))
				return nil
			}

			if recursive {
				fmt.Println("The recursive flag has been removed and is hardcoded to true without explicitly specifying it.")
				return nil
			}

			readers, err := gatherFiles(cmd, args)
			if err != nil {
				return err
			}
			cancel := make(chan struct{})

			// We use the "namespace" arg that's provided as part of root istioctl as a flag for specifying what namespace to use
			// for file resources that don't have one specified.
			selectedNamespace = ctx.Namespace()
			if useKube {
				// apply default namespace if not specified and useKube is true
				selectedNamespace = ctx.NamespaceOrDefault(selectedNamespace)
				if selectedNamespace != "" {
					client, err := ctx.CLIClient()
					if err != nil {
						return err
					}
					_, err = client.Kube().CoreV1().Namespaces().Get(context.TODO(), selectedNamespace, metav1.GetOptions{})
					if errors.IsNotFound(err) {
						fmt.Fprintf(cmd.ErrOrStderr(), "namespace %q not found\n", ctx.Namespace())
						return nil
					} else if err != nil {
						return err
					}
				}
			}

			// If we've explicitly asked for all namespaces, blank the selectedNamespace var out
			// If the user hasn't specified a namespace, use the default namespace
			if allNamespaces {
				selectedNamespace = ""
			} else if selectedNamespace == "" {
				selectedNamespace = metav1.NamespaceDefault
			}

			combinedAnalyzers := analyzers.AllCombined()
			if len(selectedAnalyzers) != 0 {
				combinedAnalyzers = analyzers.NamedCombined(selectedAnalyzers...)
			}

			sa := local.NewIstiodAnalyzer(combinedAnalyzers,
				resource.Namespace(selectedNamespace),
				resource.Namespace(ctx.IstioNamespace()), nil)

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

			shouldPrintCluster := false
			// If we're using kube, use that as a base source.
			if useKube {
				clients, err := getClients(ctx)
				if err != nil {
					return err
				}
				// If there are multiple clients, we think it's a multi-cluster scenario.
				if len(clients) > 1 {
					shouldPrintCluster = true
				}
				for _, c := range clients {
					k := kube.EnableCrdWatcher(c.client)
					sa.AddRunningKubeSourceWithRevision(k, revisionSpecified, c.remote, excludeNamespaces...)
				}
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

			if shouldPrintCluster {
				for i := range outputMessages {
					m := &outputMessages[i]
					if m.Resource != nil && m.Resource.Origin.ClusterName().String() != "" {
						m.PrintCluster = true
					}
				}
			}

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
						if len(files) > 1 {
							fmt.Fprintf(cmd.ErrOrStderr(), "\u2714 No validation issues found when analyzing:\n  - %s\n", strings.Join(files, "\n  - "))
						} else {
							fmt.Fprintf(cmd.ErrOrStderr(), "\u2714 No validation issues found when analyzing %s.\n", strings.Join(files, "\n"))
						}
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

	analysisDefaultExclude := strings.Join(sets.SortedList(inject.IgnoredNamespaces), ",")
	analysisCmd.PersistentFlags().BoolVarP(&listAnalyzers, "list-analyzers", "L", false,
		"List the analyzers available to run. Suppresses normal execution.")
	analysisCmd.PersistentFlags().StringSliceVar(&excludeNamespaces, "exclude-namespaces", nil, "Skip these namespace analysis, default="+analysisDefaultExclude)
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
		"[Removed: The recursive flag has been removed and is hardcoded to true] Process directory arguments recursively.")
	analysisCmd.PersistentFlags().BoolVar(&ignoreUnknown, "ignore-unknown", false,
		"Don't complain about un-parseable input documents, for cases where analyze should run only on k8s compliant inputs.")
	analysisCmd.PersistentFlags().StringVarP(&revisionSpecified, "revision", "r", "default",
		"analyze a specific revision deployed.")
	analysisCmd.PersistentFlags().StringArrayVar(&remoteContexts, "remote-contexts", []string{},
		`Kubernetes configuration contexts for remote clusters to be used in multi-cluster analysis. Not to be confused with '--context'. `+
			"If unspecified, contexts are read from the remote secrets in the cluster.")
	analysisCmd.PersistentFlags().StringArrayVarP(&selectedAnalyzers, "analyzer", "", []string{},
		"Select specific analyzers to run. Can be repeated. If not specified, all analyzers are run. "+
			"(e.g. istioctl analyze --analyzer \"gateway.ConflictingGatewayAnalyzer\")")
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
	runtime.SetFinalizer(r, func(x *os.File) {
		err = x.Close()
		if err != nil {
			log.Infof("file : %s is not closed: %v", f, err)
		}
	})
	return local.ReaderSource{Name: f, Reader: r}, nil
}

func gatherFilesInDirectory(cmd *cobra.Command, dir string) ([]local.ReaderSource, error) {
	var readers []local.ReaderSource

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
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
		runtime.SetFinalizer(r, func(x *os.File) {
			err = x.Close()
			if err != nil {
				log.Infof("file: %s is not closed: %v", path, err)
			}
		})
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

type Client struct {
	client kube.Client
	remote bool
}

func getClients(ctx cli.Context) ([]*Client, error) {
	client, err := ctx.CLIClient()
	if err != nil {
		return nil, err
	}
	clients := []*Client{
		{
			client: client,
			remote: false,
		},
	}
	if len(remoteContexts) > 0 {
		remoteClients, err := getClientsFromContexts(ctx)
		if err != nil {
			return nil, err
		}
		clients = append(clients, remoteClients...)
		return clients, nil
	}
	secrets, err := client.Kube().CoreV1().Secrets(ctx.IstioNamespace()).List(context.Background(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", multicluster.MultiClusterSecretLabel, "true"),
	})
	if err != nil {
		return nil, err
	}
	for _, s := range secrets.Items {
		for _, cfg := range s.Data {
			clientConfig, err := clientcmd.NewClientConfigFromBytes(cfg)
			if err != nil {
				return nil, err
			}
			rawConfig, err := clientConfig.RawConfig()
			if err != nil {
				return nil, err
			}
			curContext := rawConfig.Contexts[rawConfig.CurrentContext]
			if curContext == nil {
				continue
			}
			client, err := kube.NewCLIClient(clientConfig,
				kube.WithRevision(revisionSpecified),
				kube.WithCluster(cluster.ID(curContext.Cluster)))
			if err != nil {
				return nil, err
			}
			clients = append(clients, &Client{
				client: client,
				remote: true,
			})
		}
	}
	return clients, nil
}

func getClientsFromContexts(ctx cli.Context) ([]*Client, error) {
	var clients []*Client
	remoteClients, err := ctx.CLIClientsForContexts(remoteContexts)
	if err != nil {
		return nil, err
	}
	for _, c := range remoteClients {
		clients = append(clients, &Client{
			client: c,
			remote: true,
		})
	}
	return clients, nil
}
