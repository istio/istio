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

	"github.com/spf13/cobra"

	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/tools/bug-report/pkg/archive"
	cluster2 "istio.io/istio/tools/bug-report/pkg/cluster"
	"istio.io/istio/tools/bug-report/pkg/config"
	"istio.io/istio/tools/bug-report/pkg/content"
	"istio.io/istio/tools/bug-report/pkg/filter"
	"istio.io/istio/tools/bug-report/pkg/kubeclient"
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
	bugReportDefaultIstioNamespace = "istio-system"
	bugReportDefaultInclude        = []string{""}
	bugReportDefaultExclude        = []string{"kube-system,kube-public"}
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

var (
	// Logs, along with stats and importance metrics. Key is path (namespace/deployment/pod/cluster) which can be
	// parsed with ParsePath.
	logs       = make(map[string]string)
	stats      = make(map[string]*processlog.Stats)
	importance = make(map[string]int)
	// Aggregated errors for all fetch operations.
	gErrors util.Errors
	lock    = sync.RWMutex{}
)

func runBugReportCommand(_ *cobra.Command) error {
	// Default of "." doesn't work for K8s names.
	util.PathSeparator = "/"

	config, err := parseConfig()
	if err != nil {
		return err
	}

	_, clientset, err := kubeclient.New(config.KubeConfigPath, config.Context)
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

	gatherInfo(config, resources, paths)
	if len(gErrors) != 0 {
		log.Errora(gErrors.ToError())
	}

	// TODO: sort by importance and discard any over the size limit.
	for path, text := range logs {
		namespace, _, pod, _, err := cluster2.ParsePath(path)
		if err != nil {
			log.Errorf(err.Error())
			continue
		}
		writeFile(archive.ProxyLogPath(tempDir, namespace, pod), text)
	}
	return nil
}

// gatherInfo fetches all logs, resources, debug etc. using goroutines.
// proxy logs and info are saved in logs/stats/importance global maps.
// Errors are reported through gErrors.
func gatherInfo(config *config.BugReportConfig, resources *cluster2.Resources, paths []string) {
	var wg sync.WaitGroup

	getSecrets(config, &wg)
	getDescribePods(config, &wg)

	getFromCluster(config, &wg, "resources", content.GetK8sResources)
	getFromCluster(config, &wg, "crs", content.GetCRs)
	getFromCluster(config, &wg, "events", content.GetEvents)
	getFromCluster(config, &wg, "cluster-info", content.GetClusterInfo)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(config.CommandTimeout))
	for _, p := range paths {
		p := p
		namespace, _, pod, container, err := cluster2.ParsePath(p)
		if err != nil {
			log.Error(err.Error())
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			var wg2 sync.WaitGroup

			switch {
			case container == "istio-proxy":
				getCoreDumps(config, namespace, pod, container, &wg2)
				getProxyLogs(ctx, config, resources, p, namespace, pod, container, &wg2)

			case strings.HasPrefix(pod, "istiod-") && container == "discovery":
				getIstiodDebug(config, namespace, pod, &wg2)
				getIstiodLogs(ctx, config, resources, namespace, pod, &wg2)
			}
			wg2.Wait()
		}()
	}
	go func() {
		time.Sleep(time.Duration(config.CommandTimeout))
		cancel()
	}()

	// Not all items are subject to timeout. Proceed only if the non-cancellable items have completed.
	wg.Wait()
}

func getFromCluster(config *config.BugReportConfig, wg *sync.WaitGroup, fileName string, f func(bool) (string, error)) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		out, err := f(config.DryRun)
		appendGlobalErr(err)
		if err == nil {
			writeFile(archive.ClusterInfoPath(tempDir, fileName), out)
		}
	}()
}

func getSecrets(config *config.BugReportConfig, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		// TODO(mostrowski): configure secrets
		out, err := content.GetSecrets(false, config.DryRun)
		appendGlobalErr(err)
		if err == nil {
			writeFile(archive.ClusterInfoPath(tempDir, "secrets"), out)
		}
	}()
}

func getDescribePods(config *config.BugReportConfig, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		out, err := content.GetDescribePods(config.IstioNamespace, config.DryRun)
		appendGlobalErr(err)
		if err == nil {
			writeFile(archive.ClusterInfoPath(tempDir, "describe-pods"), out)
		}
	}()
}

func getCoreDumps(config *config.BugReportConfig, namespace, pod, container string, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		cds, err := content.GetCoredumps(namespace, pod, container, config.DryRun)
		appendGlobalErr(err)
		if err == nil && cds != "" {
			writeFile(archive.ProxyCoredumpPath(tempDir, namespace, pod), cds)
		}
	}()
}

func getProxyLogs(ctx context.Context, config *config.BugReportConfig, resources *cluster2.Resources, path, namespace, pod, container string, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		clog, cstat, imp, err := getLog(ctx, resources, config, namespace, pod, container)
		appendGlobalErr(err)
		lock.Lock()
		if err == nil {
			logs[path], stats[path], importance[path] = clog, cstat, imp
		}
		lock.Unlock()
	}()
}

func getIstiodLogs(ctx context.Context, config *config.BugReportConfig, resources *cluster2.Resources, namespace, pod string, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		clog, _, _, err := getLog(ctx, resources, config, namespace, pod, "discovery")
		appendGlobalErr(err)
		writeFile(archive.IstiodPath(tempDir, namespace, pod+".log"), clog)
	}()
}

func getIstiodDebug(config *config.BugReportConfig, namespace, pod string, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		info, err := content.GetIstiodInfo(namespace, pod, config.DryRun)
		appendGlobalErr(err)
		if err == nil {
			writeFile(archive.IstiodPath(tempDir, namespace, pod+".debug"), info)
		}
	}()
}

func getLog(ctx context.Context, resources *cluster2.Resources, config *config.BugReportConfig, namespace, pod, container string) (string, *processlog.Stats, int, error) {
	log.Infof("Getting logs for %s/%s/%s...", namespace, pod, container)
	previous := resources.ContainerRestarts(pod, container) > 0
	clog, err := kubectlcmd.Logs(namespace, pod, container, previous, config.DryRun)
	if err != nil {
		return "", nil, 0, err
	}
	cstat := &processlog.Stats{}
	clog, cstat, err = processlog.Process(config, clog)
	if err != nil {
		return "", nil, 0, err
	}
	return clog, cstat, cstat.Importance(), nil
}

func writeFile(path, text string) {
	if err := ioutil.WriteFile(path, []byte(text), 0755); err != nil {
		log.Errorf(err.Error())
	}
}

func appendGlobalErr(err error) {
	lock.Lock()
	gErrors = util.AppendErr(gErrors, err)
	lock.Unlock()
}
