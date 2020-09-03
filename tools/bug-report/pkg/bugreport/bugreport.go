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
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"

	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/pkg/kube"
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
)

var (
	bugReportDefaultIstioNamespace = "istio-system"
	bugReportDefaultInclude        = []string{""}
	bugReportDefaultExclude        = []string{"kube-system,kube-public"}
)

// Cmd returns a cobra command for bug-report.
func Cmd(logOpts *log.Options) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:          "bug-report",
		Short:        "Cluster information and log capture support tool.",
		SilenceUsage: true,
		Long: "This command selectively captures cluster information and logs into an archive to help " +
			"diagnose problems. It optionally uploads the archive to a GCS bucket.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBugReportCommand(cmd, logOpts)
		},
	}
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

func runBugReportCommand(_ *cobra.Command, logOpts *log.Options) error {
	if err := configLogs(logOpts); err != nil {
		return err
	}
	config, err := parseConfig()
	if err != nil {
		return err
	}

	clientConfig, clientset, err := kubeclient.New(config.KubeConfigPath, config.Context)
	if err != nil {
		return fmt.Errorf("could not initialize k8s client: %s ", err)
	}
	client, err := kube.NewExtendedClient(clientConfig, "")
	if err != nil {
		return err
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

	logAndPrintf("Fetching proxy logs for the following containers:\n\n%s\n", strings.Join(paths, "\n"))

	gatherInfo(client, config, resources, paths)
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

	outDir, err := os.Getwd()
	if err != nil {
		log.Errorf("using ./ to write archive: %s", err.Error())
		outDir = "."
	}
	outPath := filepath.Join(outDir, "bug-report.tgz")
	logAndPrintf("Creating archive at %s.\n", outPath)

	tempRoot := archive.OutputRootDir(tempDir)
	if err := archive.Create(tempRoot, outPath); err != nil {
		return err
	}
	logAndPrintf("Cleaning up temporary files in %s.\n", tempRoot)
	if err := os.RemoveAll(tempRoot); err != nil {
		return err
	}
	logAndPrintf("Done.\n")
	return nil
}

// gatherInfo fetches all logs, resources, debug etc. using goroutines.
// proxy logs and info are saved in logs/stats/importance global maps.
// Errors are reported through gErrors.
func gatherInfo(client kube.ExtendedClient, config *config.BugReportConfig, resources *cluster2.Resources, paths []string) {
	// no timeout on mandatoryWg.
	var mandatoryWg sync.WaitGroup
	cmdTimer := time.NewTimer(time.Duration(config.CommandTimeout))

	clusterDir := archive.ClusterInfoPath(tempDir)

	params := &content.Params{
		Client: client,
		DryRun: config.DryRun,
	}
	logAndPrintf("\nFetching Istio control plane information from cluster.\n\n")
	getFromCluster(content.GetK8sResources, params, clusterDir, &mandatoryWg)
	getFromCluster(content.GetCRs, params, clusterDir, &mandatoryWg)
	getFromCluster(content.GetEvents, params, clusterDir, &mandatoryWg)
	getFromCluster(content.GetClusterInfo, params, clusterDir, &mandatoryWg)
	getFromCluster(content.GetSecrets, params.SetVerbose(config.FullSecrets), clusterDir, &mandatoryWg)
	getFromCluster(content.GetDescribePods, params.SetNamespace(config.IstioNamespace), clusterDir, &mandatoryWg)

	// optionalWg is subject to timer.
	var optionalWg sync.WaitGroup
	for _, p := range paths {
		namespace, _, pod, container, err := cluster2.ParsePath(p)
		if err != nil {
			log.Error(err.Error())
			continue
		}

		cp := params.SetNamespace(namespace).SetPod(pod).SetContainer(container)
		switch {
		case container == "istio-proxy":
			getFromCluster(content.GetCoredumps, cp, archive.ProxyCoredumpPath(tempDir, namespace, pod), &mandatoryWg)
			getProxyLogs(client, config, resources, p, namespace, pod, container, &optionalWg)

		case strings.HasPrefix(pod, "istiod-") && container == "discovery":
			getFromCluster(content.GetIstiodInfo, cp, archive.IstiodPath(tempDir, namespace, pod), &mandatoryWg)
			getIstiodLogs(client, config, resources, namespace, pod, &mandatoryWg)

		}
	}

	// Not all items are subject to timeout. Proceed only if the non-cancellable items have completed.
	mandatoryWg.Wait()

	// If log fetches have completed, cancel the timeout.
	go func() {
		optionalWg.Wait()
		cmdTimer.Stop()
	}()

	// Wait for log fetches, up to the timeout.
	<-cmdTimer.C
}

// getFromCluster runs a cluster info fetching function f against the cluster and writes the results to fileName.
// Runs if a goroutine, with errors reported through gErrors.
func getFromCluster(f func(params *content.Params) (map[string]string, error), params *content.Params, dir string, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		out, err := f(params)
		appendGlobalErr(err)
		if err == nil {
			writeFiles(dir, out)
		}
	}()
}

// getProxyLogs fetches proxy logs for the given namespace/pod/container and stores the output in global structs.
// Runs if a goroutine, with errors reported through gErrors.
// TODO(stewartbutler): output the logs to a more robust/complete structure.
func getProxyLogs(client kube.ExtendedClient, config *config.BugReportConfig, resources *cluster2.Resources,
	path, namespace, pod, container string, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		clog, cstat, imp, err := getLog(client, resources, config, namespace, pod, container)
		appendGlobalErr(err)
		lock.Lock()
		if err == nil {
			logs[path], stats[path], importance[path] = clog, cstat, imp
		}
		lock.Unlock()
	}()
}

// getIstiodLogs fetches Istiod logs for the given namespace/pod and writes the output.
// Runs if a goroutine, with errors reported through gErrors.
func getIstiodLogs(client kube.ExtendedClient, config *config.BugReportConfig, resources *cluster2.Resources,
	namespace, pod string, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		clog, _, _, err := getLog(client, resources, config, namespace, pod, "discovery")
		appendGlobalErr(err)
		writeFile(archive.IstiodPath(tempDir, namespace, pod+".log"), clog)
	}()
}

// getLog fetches the logs for the given namespace/pod/container and returns the log text and stats for it.
func getLog(client kube.ExtendedClient, resources *cluster2.Resources, config *config.BugReportConfig,
	namespace, pod, container string) (string, *processlog.Stats, int, error) {
	log.Infof("Getting logs for %s/%s/%s...", namespace, pod, container)
	clog, err := kubectlcmd.Logs(client, namespace, pod, container, false, config.DryRun)
	if err != nil {
		return "", nil, 0, err
	}
	if resources.ContainerRestarts(namespace, pod, container) > 0 {
		pclog, err := kubectlcmd.Logs(client, namespace, pod, container, true, config.DryRun)
		if err != nil {
			return "", nil, 0, err
		}
		clog = "========= Previous log present (appended at the end) =========\n\n" + clog +
			"\n\n========= Previous log =========\n\n" + pclog
	}
	var cstat *processlog.Stats
	clog, cstat = processlog.Process(config, clog)
	return clog, cstat, cstat.Importance(), nil
}

func writeFiles(dir string, files map[string]string) {
	for fname, text := range files {
		writeFile(filepath.Join(dir, fname), text)
	}
}

func writeFile(path, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	mkdirOrExit(path)
	if err := ioutil.WriteFile(path, []byte(text), 0644); err != nil {
		log.Errorf(err.Error())
	}
}

func mkdirOrExit(fpath string) {
	if err := os.MkdirAll(path.Dir(fpath), 0755); err != nil {
		fmt.Printf("Could not create output directories: %s", err)
		os.Exit(-1)
	}
}

func appendGlobalErr(err error) {
	if err == nil {
		return
	}
	lock.Lock()
	gErrors = util.AppendErr(gErrors, err)
	lock.Unlock()
}

func BuildClientsFromConfig(kubeConfig []byte) (kube.Client, error) {
	if len(kubeConfig) == 0 {
		return nil, errors.New("kubeconfig is empty")
	}

	rawConfig, err := clientcmd.Load(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("kubeconfig cannot be loaded: %v", err)
	}

	if err := clientcmd.Validate(*rawConfig); err != nil {
		return nil, fmt.Errorf("kubeconfig is not valid: %v", err)
	}

	clientConfig := clientcmd.NewDefaultClientConfig(*rawConfig, &clientcmd.ConfigOverrides{})

	clients, err := kube.NewClient(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kube clients: %v", err)
	}
	return clients, nil
}

func logAndPrintf(format string, a ...interface{}) {
	fmt.Printf(format, a...)
	log.Infof(format, a...)
}

func configLogs(opt *log.Options) error {
	logDir := filepath.Join(archive.OutputRootDir(tempDir), "bug-report.log")
	mkdirOrExit(logDir)
	f, err := os.Create(logDir)
	if err != nil {
		return err
	}
	f.Close()
	op := []string{logDir}
	opt2 := *opt
	opt2.OutputPaths = op
	opt2.ErrorOutputPaths = op
	opt2.SetOutputLevel("default", log.InfoLevel)

	return log.Configure(&opt2)
}
