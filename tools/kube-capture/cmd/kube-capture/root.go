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

package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	cluster "istio.io/istio/tools/kube-capture/pkg"
	"istio.io/pkg/version"
)

const (
	kubeCaptureDefaultMaxSizeMb = 500
	kubeCaptureDefaultTimeout   = 30 * time.Minute
	kubeCaptureDefaultInclude   = "*"
	kubeCaptureDefaultExclude   = "kube-system,kube-public,istio-system"
)

var (
	kubeCaptureDefaultIstioNamespaces = []string{"istio-system"}
)

const (
	kubeCaptureHelpKubeconfig      = "Path to kube config."
	kubeCaptureHelpContext         = "Name of the kubeconfig Context to use."
	kubeCaptureHelpFilename        = "Path to a file containing configuration in YAML format."
	kubeCaptureHelpIstioNamespaces = "List of comma-separated namespaces where Istio control planes " +
		"are installed."
	kubeCaptureHelpDryRun = "Console output only, does not actually capture logs."
	kubeCaptureHelpStrict = "Ensure that the include and exclude selection specs all match at least one " +
		"cluster resource."
	kubeCaptureHelpCommandTimeout = "Maximum amount of time to spend fetching logs. When timeout is reached " +
		"only the logs captured so far are saved to the archive."
	kubeCaptureHelpMaxArchiveSizeMb = "Maximum size of the compressed archive in Mb. Logs are prioritized" +
		"according to importance heuristics."
	kubeCaptureHelpIncluded = "Spec for which pods' proxy logs to include in the archive. See 'help' for examples."
	kubeCaptureHelpExcluded = "Spec for which pods' proxy logs to exclude from the archive, after the include spec " +
		"is processed. See 'help' for examples."
	kubeCaptureHelpStartTime = "Start time for the range of log entries to include in the archive. " +
		"Default is the infinite past. If set, Since must be unset."
	kubeCaptureHelpEndTime = "End time for the range of log entries to include in the archive. Default is now."
	kubeCaptureHelpSince   = "How far to go back in time from end-time for log entries to include in the archive. " +
		"Default is infinity. If set, start-time must be unset."
	kubeCaptureHelpCriticalErrors = "List of comma separated glob patters to match against log error strings. " +
		"If any pattern matches an error in the log, the logs is given the highest priority for archive inclusion."
	kubeCaptureHelpWhitelistedErrors = "List of comma separated glob patters to match against log error strings. " +
		"Any error matching these patters is ignored when calculating the log importance heuristic."
	kubeCaptureHelpGCSURL      = "URL of the GCS bucket where the archive is uploaded."
	kubeCaptureHelpUploadToGCS = "Upload archive to GCS bucket. If gcs-url is unset, a new bucket is created."
)

var (
	startTime, endTime, included, excluded string
	commandTimeout, since                  time.Duration
	gConfig                                = &KubeCaptureConfig{}
)

func addFlags(cmd *cobra.Command, args *KubeCaptureConfig) {
	// k8s client config
	cmd.PersistentFlags().StringVarP(&args.KubeConfigPath, "kubeconfig", "c", "", kubeCaptureHelpKubeconfig)
	cmd.PersistentFlags().StringVar(&args.Context, "Context", "", kubeCaptureHelpContext)

	// dry run and validation
	cmd.PersistentFlags().BoolVarP(&args.DryRun, "dry-run", "", false, kubeCaptureHelpDryRun)
	cmd.PersistentFlags().BoolVar(&args.Strict, "Strict", false, kubeCaptureHelpStrict)

	// istio namespaces
	cmd.PersistentFlags().StringSliceVarP(&args.IstioNamespaces, "namespaces", "n", kubeCaptureDefaultIstioNamespaces, kubeCaptureHelpIstioNamespaces)

	// timeouts and max sizes
	cmd.PersistentFlags().DurationVar(&commandTimeout, "timeout", kubeCaptureDefaultTimeout, kubeCaptureHelpCommandTimeout)
	cmd.PersistentFlags().Int32Var(&args.MaxArchiveSizeMb, "max-size", kubeCaptureDefaultMaxSizeMb, kubeCaptureHelpMaxArchiveSizeMb)

	// include / exclude specs
	cmd.PersistentFlags().StringVarP(&included, "include", "i", kubeCaptureDefaultInclude, kubeCaptureHelpIncluded)
	cmd.PersistentFlags().StringVarP(&excluded, "exclude", "e", kubeCaptureDefaultExclude, kubeCaptureHelpExcluded)

	// log time ranges
	cmd.PersistentFlags().StringVar(&startTime, "start-time", "", kubeCaptureHelpStartTime)
	cmd.PersistentFlags().StringVar(&endTime, "end-time", "", kubeCaptureHelpEndTime)
	cmd.PersistentFlags().DurationVar(&since, "duration", 0, kubeCaptureHelpSince)

	// log error control
	cmd.PersistentFlags().StringSliceVar(&args.CriticalErrors, "critical-errs", nil, kubeCaptureHelpCriticalErrors)
	cmd.PersistentFlags().StringSliceVar(&args.WhitelistedErrors, "whitelist-errs", nil, kubeCaptureHelpWhitelistedErrors)

	// archive upload control
	cmd.PersistentFlags().StringVar(&args.Context, "gcs-url", "", kubeCaptureHelpGCSURL)
	cmd.PersistentFlags().BoolVar(&args.Strict, "upload", false, kubeCaptureHelpUploadToGCS)
}

// GetRootCmd returns the root of the cobra command-tree.
func GetRootCmd(args []string) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:          "kube-capture",
		Short:        "Cluster information and log capture support tool.",
		SilenceUsage: true,
		Long: "This command selectively captures cluster information and logs into an archive to help " +
			"diagnose problems. It optionally uploads the archive to a GCS bucket.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKubeCaptureCommand(cmd, gConfig)
		},
	}
	rootCmd.SetArgs(args)
	rootCmd.AddCommand(version.CobraCommand())
	addFlags(rootCmd, gConfig)

	return rootCmd
}

func runKubeCaptureCommand(cmd *cobra.Command, config *KubeCaptureConfig) error {
	parseTimes(gConfig, startTime, endTime)
	//ValidateKubeCaptureConfig(gConfig)
	rest, clientset, err := InitK8SRestClient(config.KubeConfigPath, config.Context)
	if err != nil {
		return fmt.Errorf("could not initialize k8s client: %s ", err)
	}
	resources, err := cluster.GetClusterResources(rest, clientset)
	if err != nil {
		return err
	}

	paths, err := GetMatchingPaths(config, resources)
	if err != nil {
		return err
	}

	fmt.Printf("Fetching logs for the following containers:\n\n%s", strings.Join(paths, "\n"))

	// Download logs for containers in paths.

	return nil
}

func parseTimes(args *KubeCaptureConfig, startTime, endTime string) {
	args.EndTime = time.Now()
	if endTime != "" {
		var err error
		args.EndTime, err = time.Parse(time.RFC3339, endTime)
		if err != nil {
			fmt.Printf("Bad format for end-time: %s, expect RFC3339 e.g. %s", endTime, time.RFC3339)
			os.Exit(-1)
		}
	}
	if args.Since != 0 {
		if startTime != "" {
			fmt.Println("Only one --start-time or --Since may be set.")
			os.Exit(-1)
		}
		args.StartTime = args.EndTime.Add(-1 * time.Duration(args.Since))
	} else {
		var err error
		args.StartTime, err = time.Parse(time.RFC3339, startTime)
		if err != nil {
			fmt.Printf("Bad format for start-time: %s, expect RFC3339 e.g. %s", startTime, time.RFC3339)
			os.Exit(-1)
		}
	}
}
