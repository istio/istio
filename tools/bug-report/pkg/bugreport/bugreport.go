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
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/tools/bug-report/pkg/client"
	cluster2 "istio.io/istio/tools/bug-report/pkg/cluster"
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
			previous := resources.ContainerRestarts(pod, container) > 0
			clog, err := kubectlcmd.Logs(namespace, pod, container, previous, config.DryRun)
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
