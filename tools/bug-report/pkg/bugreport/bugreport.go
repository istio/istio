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

package bugreport

import (
	"context"
	"fmt"
	"io/ioutil"
	"strings"
	"sync"
	"time"

	"github.com/ghodss/yaml"
	"github.com/spf13/cobra"

	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/tools/bug-report/pkg/client"
	cluster2 "istio.io/istio/tools/bug-report/pkg/cluster"
	config2 "istio.io/istio/tools/bug-report/pkg/config"
	"istio.io/istio/tools/bug-report/pkg/filter"
	"istio.io/istio/tools/bug-report/pkg/kubectlcmd"
	"istio.io/istio/tools/bug-report/pkg/processlog"
	"istio.io/pkg/log"
	"istio.io/pkg/version"
)

const (
	bugReportDefaultMaxSizeMb = 500
	bugReportDefaultTimeout   = 30 * time.Minute
	bugReportDefaultTempDir   = "/tmp/bug-report"
)

var (
	bugReportDefaultIstioNamespaces = []string{"istio-system"}
	bugReportDefaultInclude         = []string{"*"}
	bugReportDefaultExclude         = []string{"kube-system,kube-public"}
)

const (
	bugReportHelpKubeconfig      = "Path to kube config."
	bugReportHelpContext         = "Name of the kubeconfig Context to use."
	bugReportHelpFilename        = "Path to a file containing configuration in YAML format."
	bugReportHelpIstioNamespaces = "List of comma-separated namespaces where Istio control planes " +
		"are installed."
	bugReportHelpDryRun         = "Console output only, does not actually capture logs."
	bugReportHelpCommandTimeout = "Maximum amount of time to spend fetching logs. When timeout is reached " +
		"only the logs captured so far are saved to the archive."
	bugReportHelpMaxArchiveSizeMb = "Maximum size of the compressed archive in Mb. Logs are prioritized" +
		"according to importance heuristics."
	bugReportHelpInclude = "Spec for which pods' proxy logs to include in the archive. See 'help' for examples."
	bugReportHelpExclude = "Spec for which pods' proxy logs to exclude from the archive, after the include spec " +
		"is processed. See 'help' for examples."
	bugReportHelpStartTime = "Start time for the range of log entries to include in the archive. " +
		"Default is the infinite past. If set, Since must be unset."
	bugReportHelpEndTime = "End time for the range of log entries to include in the archive. Default is now."
	bugReportHelpSince   = "How far to go back in time from end-time for log entries to include in the archive. " +
		"Default is infinity. If set, start-time must be unset."
	bugReportHelpCriticalErrors = "List of comma separated glob patters to match against log error strings. " +
		"If any pattern matches an error in the log, the logs is given the highest priority for archive inclusion."
	bugReportHelpWhitelistedErrors = "List of comma separated glob patters to match against log error strings. " +
		"Any error matching these patters is ignored when calculating the log importance heuristic."
	bugReportHelpGCSURL      = "URL of the GCS bucket where the archive is uploaded."
	bugReportHelpUploadToGCS = "Upload archive to GCS bucket. If gcs-url is unset, a new bucket is created."
	bugReportHelpTempDir     = "Set a specific directory for temporary artifact storage."
)

var (
	startTime, endTime, configFile, tempDir string
	included, excluded                      []string
	commandTimeout, since                   time.Duration
	gConfig                                 = &config2.BugReportConfig{}
)

func addFlags(cmd *cobra.Command, args *config2.BugReportConfig) {
	// k8s client config
	cmd.PersistentFlags().StringVarP(&args.KubeConfigPath, "kubeconfig", "c", "", bugReportHelpKubeconfig)
	cmd.PersistentFlags().StringVar(&args.Context, "context", "", bugReportHelpContext)

	// input config
	cmd.PersistentFlags().StringVarP(&configFile, "filename", "f", "", bugReportHelpFilename)

	// dry run
	cmd.PersistentFlags().BoolVarP(&args.DryRun, "dry-run", "", false, bugReportHelpDryRun)

	// istio namespaces
	cmd.PersistentFlags().StringSliceVarP(&args.IstioNamespaces, "namespaces", "n", bugReportDefaultIstioNamespaces, bugReportHelpIstioNamespaces)

	// timeouts and max sizes
	cmd.PersistentFlags().DurationVar(&commandTimeout, "timeout", bugReportDefaultTimeout, bugReportHelpCommandTimeout)
	cmd.PersistentFlags().Int32Var(&args.MaxArchiveSizeMb, "max-size", bugReportDefaultMaxSizeMb, bugReportHelpMaxArchiveSizeMb)

	// include / exclude specs
	cmd.PersistentFlags().StringSliceVarP(&included, "include", "i", bugReportDefaultInclude, bugReportHelpInclude)
	cmd.PersistentFlags().StringSliceVarP(&excluded, "exclude", "e", bugReportDefaultExclude, bugReportHelpExclude)

	// log time ranges
	cmd.PersistentFlags().StringVar(&startTime, "start-time", "", bugReportHelpStartTime)
	cmd.PersistentFlags().StringVar(&endTime, "end-time", "", bugReportHelpEndTime)
	cmd.PersistentFlags().DurationVar(&since, "duration", 0, bugReportHelpSince)

	// log error control
	cmd.PersistentFlags().StringSliceVar(&args.CriticalErrors, "critical-errs", nil, bugReportHelpCriticalErrors)
	cmd.PersistentFlags().StringSliceVar(&args.WhitelistedErrors, "whitelist-errs", nil, bugReportHelpWhitelistedErrors)

	// archive and upload control
	cmd.PersistentFlags().StringVar(&args.Context, "gcs-url", "", bugReportHelpGCSURL)
	cmd.PersistentFlags().BoolVar(&args.UploadToGCS, "upload", false, bugReportHelpUploadToGCS)

	// output/working dir
	cmd.PersistentFlags().StringVar(&tempDir, "dir", bugReportDefaultTempDir, bugReportHelpTempDir)
}

// GetRootCmd returns the root of the cobra command-tree.
func GetRootCmd(args []string) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:          "bug-report",
		Short:        "Cluster information and log capture support tool.",
		SilenceUsage: true,
		Long: "This command selectively captures cluster information and logs into an archive to help " +
			"diagnose problems. It optionally uploads the archive to a GCS bucket.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBugReportCommand(cmd)
		},
	}
	rootCmd.SetArgs(args)
	rootCmd.AddCommand(version.CobraCommand())
	addFlags(rootCmd, gConfig)

	return rootCmd
}

func runBugReportCommand(_ *cobra.Command) error {
	config, err := parseConfig()
	if err != nil {
		return err
	}

	_, clientset, err := client.InitK8SRestClient(config.KubeConfigPath, config.Context)
	if err != nil {
		return fmt.Errorf("could not initialize k8s client: %s ", err)
	}
	resources, err := cluster2.GetClusterResources(context.Background(), clientset)
	if err != nil {
		return err
	}

	log.Infof("Cluster resource tree:\n\n%s\n\n", resources)
	paths, err := filter.GetMatchingPaths(config, resources)
	if err != nil {
		return err
	}

	log.Infof("Fetching logs for the following containers:\n\n%s\n", strings.Join(paths, "\n"))

	var errs util.Errors
	logs := make(map[string]string)
	stats := make(map[string]*processlog.Stats)
	importance := make(map[string]int)
	lock := sync.RWMutex{}
	for _, p := range paths {
		p := p
		go func() {
			cv := strings.Split(p, ".")
			namespace, pod, container := cv[0], cv[2], cv[3]
			clog, err := kubectlcmd.Logs(namespace, pod, container, config.DryRun)
			if err != nil {
				lock.Lock()
				errs = util.AppendErr(errs, err)
				lock.Unlock()
				return
			}
			cstat := &processlog.Stats{}
			clog, cstat, err = processlog.Process(config, clog)
			lock.Lock()
			logs[p], stats[p], importance[p] = clog, cstat, cstat.Importance()
			lock.Unlock()
		}()
	}

	return nil
}

func parseConfig() (*config2.BugReportConfig, error) {
	config := &config2.BugReportConfig{}
	if configFile != "" {
		b, err := ioutil.ReadFile(configFile)
		if err != nil {
			return nil, err
		}
		if err := yaml.Unmarshal(b, config); err != nil {
			return nil, err
		}
	}
	if err := parseTimes(config, startTime, endTime); err != nil {
		log.Fatal(err.Error())
	}
	for _, s := range included {
		ss := &config2.SelectionSpec{}
		if err := ss.UnmarshalJSON([]byte(s)); err != nil {
			return nil, err
		}
		config.Include = append(config.Include, ss)
	}
	for _, s := range excluded {
		ss := &config2.SelectionSpec{}
		if err := ss.UnmarshalJSON([]byte(s)); err != nil {
			return nil, err
		}
		config.Exclude = append(config.Exclude, ss)
	}

	return config, nil
}

func parseTimes(config *config2.BugReportConfig, startTime, endTime string) error {
	config.EndTime = time.Now()
	if endTime != "" {
		var err error
		config.EndTime, err = time.Parse(time.RFC3339, endTime)
		if err != nil {
			return fmt.Errorf("bad format for end-time: %s, expect RFC3339 e.g. %s", endTime, time.RFC3339)
		}
	}
	if config.Since != 0 {
		if startTime != "" {
			return fmt.Errorf("only one --start-time or --Since may be set")
		}
		config.StartTime = config.EndTime.Add(-1 * time.Duration(config.Since))
	} else {
		var err error
		if startTime == "" {
			config.StartTime = time.Time{}
		} else {
			config.StartTime, err = time.Parse(time.RFC3339, startTime)
			if err != nil {
				return fmt.Errorf("bad format for start-time: %s, expect RFC3339 e.g. %s", startTime, time.RFC3339)
			}
		}
	}
	return nil
}
