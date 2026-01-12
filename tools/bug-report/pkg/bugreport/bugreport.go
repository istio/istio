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
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	label2 "istio.io/api/label"
	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/util/ambient"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/proxy"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/version"
	"istio.io/istio/tools/bug-report/pkg/archive"
	cluster2 "istio.io/istio/tools/bug-report/pkg/cluster"
	"istio.io/istio/tools/bug-report/pkg/common"
	"istio.io/istio/tools/bug-report/pkg/config"
	"istio.io/istio/tools/bug-report/pkg/content"
	"istio.io/istio/tools/bug-report/pkg/filter"
	"istio.io/istio/tools/bug-report/pkg/kubeclient"
	"istio.io/istio/tools/bug-report/pkg/kubectlcmd"
	"istio.io/istio/tools/bug-report/pkg/processlog"
)

const (
	bugReportDefaultTimeout = 30 * time.Minute
)

var (
	bugReportDefaultIstioNamespace = "istio-system"
	bugReportDefaultInclude        = []string{""}
	bugReportDefaultExclude        = []string{strings.Join(sets.SortedList(inject.IgnoredNamespaces), ",")}
)

// Cmd returns a cobra command for bug-report.
func Cmd(ctx cli.Context, logOpts *log.Options) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:          "bug-report",
		Short:        "Cluster information and log capture support tool.",
		SilenceUsage: true,
		Long: `bug-report selectively captures cluster information and logs into an archive to help diagnose problems.
Proxy logs can be filtered using:
  --include|--exclude ns1,ns2.../dep1,dep2.../pod1,pod2.../lbl1=val1,lbl2=val2.../ann1=val1,ann2=val2.../cntr1,cntr...
where ns=namespace, dep=deployment, lbl=label, ann=annotation, cntr=container

The filter spec is interpreted as 'must be in (ns1 OR ns2) AND (dep1 OR dep2) AND (cntr1 OR cntr2)...'
The log will be included only if the container matches at least one include filter and does not match any exclude filters.
All parts of the filter are optional and can be omitted e.g. ns1//pod1 filters only for namespace ns1 and pod1.
All names except label and annotation keys support '*' glob matching pattern.

e.g.
--include ns1,ns2 (only namespaces ns1 and ns2)
--include n*//p*/l=v* (pods with name beginning with 'p' in namespaces beginning with 'n' and having label 'l' with value beginning with 'v'.)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBugReportCommand(ctx, cmd, logOpts)
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

func runBugReportCommand(ctx cli.Context, _ *cobra.Command, logOpts *log.Options) error {
	runner := kubectlcmd.NewRunner(gConfig.RequestConcurrency)
	runner.ReportRunningTasks()
	if err := configLogs(logOpts); err != nil {
		return err
	}
	config, err := parseConfig()
	if err != nil {
		return err
	}
	clusterCtxStr := ""
	if config.Context == "" {
		var err error
		clusterCtxStr, err = content.GetClusterContext(runner, config.KubeConfigPath)
		if err != nil {
			return err
		}
	} else {
		clusterCtxStr = config.Context
	}

	common.LogAndPrintf("\nTarget cluster context: %s\n", clusterCtxStr)
	common.LogAndPrintf("Running with the following config: \n\n%s\n\n", config)

	restConfig, clientset, err := kubeclient.New(config.KubeConfigPath, config.Context)
	if err != nil {
		return fmt.Errorf("could not initialize k8s client: %s ", err)
	}
	client, err := kube.NewCLIClient(kube.NewClientConfigForRestConfig(restConfig))
	if err != nil {
		return err
	}
	common.LogAndPrintf("\nCluster endpoint: %s\n", client.RESTConfig().Host)
	runner.SetClient(client)

	clusterResourcesCtx, getClusterResourcesCancel := context.WithTimeout(context.Background(), commandTimeout)
	curTime := time.Now()
	defer func() {
		if time.Until(curTime.Add(commandTimeout)) < 0 {
			message := "Timeout when running bug report command, please using --include or --exclude to filter"
			common.LogAndPrintf("%s", message)
		}
		getClusterResourcesCancel()
	}()
	resources, err := cluster2.GetClusterResources(clusterResourcesCtx, clientset, config)
	if err != nil {
		return err
	}
	logRuntime(curTime, "Done collecting cluster resource")

	dumpRevisionsAndVersions(ctx, resources, config.IstioNamespace, config.DryRun)

	log.Infof("Cluster resource tree:\n\n%s\n\n", resources)
	paths, err := filter.GetMatchingPaths(config, resources)
	if err != nil {
		return err
	}

	common.LogAndPrintf("\n\nFetching logs for the following containers:\n\n%s\n", strings.Join(paths, "\n"))

	gatherInfo(runner, config, resources, paths)
	if len(gErrors) != 0 {
		log.Error(gErrors.ToError())
	}

	// TODO: sort by importance and discard any over the size limit.
	for path, text := range logs {
		namespace, _, pod, _, err := cluster2.ParsePath(path)
		if err != nil {
			log.Errorf(err.Error())
			continue
		}
		writeFile(filepath.Join(archive.ProxyOutputPath(tempDir, namespace, pod), common.ProxyContainerName+".log"), text, config.DryRun)
	}

	logRuntime(curTime, "Done with bug-report command before generating the archive file")

	outDir, err := os.Getwd()
	if err != nil {
		log.Errorf("using ./ to write archive: %s", err.Error())
		outDir = "."
	}
	if outputDir != "" {
		outDir = outputDir
	}
	outPath := filepath.Join(outDir, "bug-report.tar.gz")

	if !config.DryRun {
		common.LogAndPrintf("Creating an archive at %s.\n", outPath)
		archiveDir := archive.DirToArchive(tempDir)
		if tempDir != "" {
			archiveDir = tempDir
		}
		curTime = time.Now()
		err := archive.Create(archiveDir, outPath)
		fmt.Printf("Time used for creating the tar file is %v.\n", time.Since(curTime))
		if err != nil {
			return err
		}
		common.LogAndPrintf("Cleaning up temporary files in %s.\n", archiveDir)
		if err := os.RemoveAll(archiveDir); err != nil {
			return err
		}
	} else {
		common.LogAndPrintf("Dry run, skipping archive creation at %s.\n", outPath)
	}
	common.LogAndPrintf("Done.\n")
	return nil
}

func dumpRevisionsAndVersions(ctx cli.Context, resources *cluster2.Resources, istioNamespace string, dryRun bool) {
	defer logRuntime(time.Now(), "Done getting control plane revisions/versions")

	text := ""
	text += fmt.Sprintf("CLI version:\n%s\n\n", version.Info.LongForm())

	revisions := getIstioRevisions(resources)
	istioVersions, proxyVersions := getIstioVersions(ctx, istioNamespace, revisions)
	text += "The following Istio control plane revisions/versions were found in the cluster:\n"
	for rev, ver := range istioVersions {
		text += fmt.Sprintf("Revision %s:\n%s\n\n", rev, ver)
	}
	text += "The following proxy revisions/versions were found in the cluster:\n"
	for rev, ver := range proxyVersions {
		text += fmt.Sprintf("Revision %s: Versions {%s}\n", rev, strings.Join(ver, ", "))
	}
	common.LogAndPrintf("%s", text)
	writeFile(filepath.Join(archive.OutputRootDir(tempDir), "versions"), text, dryRun)
}

// getIstioRevisions returns a slice with all Istio revisions detected in the cluster.
func getIstioRevisions(resources *cluster2.Resources) []string {
	revMap := sets.New[string]()
	for _, podLabels := range resources.Labels {
		for label, value := range podLabels {
			if label == label2.IoIstioRev.Name {
				revMap.Insert(value)
			}
		}
	}
	for _, podAnnotations := range resources.Annotations {
		for annotation, value := range podAnnotations {
			if annotation == label2.IoIstioRev.Name {
				revMap.Insert(value)
			}
		}
	}
	return sets.SortedList(revMap)
}

// getIstioVersions returns a mapping of revision to aggregated version string for Istio components and revision to
// slice of versions for proxies. Any errors are embedded in the revision strings.
func getIstioVersions(ctx cli.Context, istioNamespace string, revisions []string) (map[string]string, map[string][]string) {
	istioVersions := make(map[string]string)
	proxyVersionsMap := make(map[string]sets.String)
	proxyVersions := make(map[string][]string)
	for _, revision := range revisions {
		client, err := ctx.CLIClientWithRevision(revision)
		if err != nil {
			log.Error(err)
			continue
		}
		istioVersions[revision] = getIstioVersion(client, istioNamespace)
		proxyInfo, err := proxy.GetProxyInfo(client, istioNamespace)
		if err != nil {
			log.Error(err)
			continue
		}
		for _, pi := range *proxyInfo {
			sets.InsertOrNew(proxyVersionsMap, revision, pi.IstioVersion)
		}
	}
	for revision, vmap := range proxyVersionsMap {
		for v := range vmap {
			proxyVersions[revision] = append(proxyVersions[revision], v)
		}
	}
	return istioVersions, proxyVersions
}

func getIstioVersion(kubeClient kube.CLIClient, istioNamespace string) string {
	versions, err := kubeClient.GetIstioVersions(context.TODO(), istioNamespace)
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("%#v", versions)
}

// gatherInfo fetches all logs, resources, debug etc. using goroutines.
// proxy logs and info are saved in logs/stats/importance global maps.
// Errors are reported through gErrors.
func gatherInfo(runner *kubectlcmd.Runner, config *config.BugReportConfig, resources *cluster2.Resources, paths []string) {
	// no timeout on mandatoryWg.
	var mandatoryWg sync.WaitGroup
	cmdTimer := time.NewTimer(time.Duration(config.CommandTimeout))
	beginTime := time.Now()

	client, err := kube.NewCLIClient(kube.BuildClientCmd(config.KubeConfigPath, config.Context))
	if err != nil {
		appendGlobalErr(err)
	}

	clusterDir := archive.ClusterInfoPath(tempDir)

	params := &content.Params{
		Runner:      runner,
		DryRun:      config.DryRun,
		KubeConfig:  config.KubeConfigPath,
		KubeContext: config.Context,
	}
	common.LogAndPrintf("\nFetching Istio control plane information from cluster.\n\n")
	getFromCluster(content.GetK8sResources, params, clusterDir, &mandatoryWg)
	getFromCluster(content.GetCRs, params, clusterDir, &mandatoryWg)
	getFromCluster(content.GetEvents, params, clusterDir, &mandatoryWg)
	getFromCluster(content.GetClusterInfo, params, clusterDir, &mandatoryWg)
	getFromCluster(content.GetNodeInfo, params, clusterDir, &mandatoryWg)
	getFromCluster(content.GetSecrets, params.SetVerbose(config.FullSecrets), clusterDir, &mandatoryWg)
	getFromCluster(content.GetPodInfo, params.SetIstioNamespace(config.IstioNamespace), clusterDir, &mandatoryWg)

	common.LogAndPrintf("\nFetching CNI logs from cluster.\n\n")
	for _, cniPod := range resources.CniPod {
		getCniLogs(runner, config, resources, cniPod.Namespace, cniPod.Name, &mandatoryWg)
	}

	// optionalWg is subject to timer.
	var optionalWg sync.WaitGroup
	for _, p := range paths {
		namespace, _, pod, container, err := cluster2.ParsePath(p)
		if err != nil {
			log.Error(err.Error())
			continue
		}

		cp := params.SetNamespace(namespace).SetPod(pod).SetContainer(container)
		proxyDir := archive.ProxyOutputPath(tempDir, namespace, pod)
		switch {
		case common.IsProxyContainer(params.ClusterVersion, container):
			if !ambient.IsZtunnelPod(client, pod, namespace) {
				getFromCluster(content.GetCoredumps, cp, filepath.Join(proxyDir, "cores"), &mandatoryWg)
				getFromCluster(content.GetNetstat, cp, proxyDir, &mandatoryWg)
				getFromCluster(content.GetProxyInfo, cp.SetProxyAdminPort(config.ProxyAdminPort), archive.ProxyOutputPath(tempDir, namespace, pod), &optionalWg)
				getProxyLogs(runner, config, resources, p, namespace, pod, container, &optionalWg)
			} else {
				getFromCluster(content.GetNetstat, cp, proxyDir, &mandatoryWg)
				getFromCluster(content.GetZtunnelInfo, cp.SetProxyAdminPort(config.ProxyAdminPort), archive.ProxyOutputPath(tempDir, namespace, pod), &optionalWg)
				getProxyLogs(runner, config, resources, p, namespace, pod, container, &optionalWg)
			}
		case resources.IsDiscoveryContainer(params.ClusterVersion, namespace, pod, container):
			getFromCluster(content.GetIstiodInfo, cp, archive.IstiodPath(tempDir, namespace, pod), &mandatoryWg)
			getIstiodLogs(runner, config, resources, namespace, pod, &mandatoryWg)

		case common.IsOperatorContainer(params.ClusterVersion, container):
			getOperatorLogs(runner, config, resources, namespace, pod, &optionalWg)
		}
	}

	// Not all items are subject to timeout. Proceed only if the non-cancellable items have completed.
	mandatoryWg.Wait()

	// If log fetches have completed, cancel the timeout.
	go func() {
		optionalWg.Wait()
		cmdTimer.Reset(0)
	}()

	// Wait for log fetches, up to the timeout.
	<-cmdTimer.C

	// Find the timeout duration left for the analysis process.
	analyzeTimeout := time.Until(beginTime.Add(time.Duration(config.CommandTimeout)))

	// Analyze runs many queries internally, so run these queries sequentially and after everything else has finished.
	runAnalyze(config, params, analyzeTimeout)
}

// getFromCluster runs a cluster info fetching function f against the cluster and writes the results to fileName.
// Runs if a goroutine, with errors reported through gErrors.
func getFromCluster(f func(params *content.Params) (map[string]string, error), params *content.Params, dir string, wg *sync.WaitGroup) {
	startTime := time.Now()
	wg.Add(1)
	log.Infof("Waiting on %s", runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name())
	go func() {
		defer func() {
			wg.Done()
			logRuntime(startTime, "Done getting from cluster for %v", runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name())
		}()

		out, err := f(params)
		appendGlobalErr(filterUnknownBinaryErrors(err))
		if err == nil {
			writeFiles(dir, out, params.DryRun)
		}
		log.Infof("Done with %s", runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name())
	}()
}

// filterUnknownBinaryErrors ignores errors about not finding a binary
// This is expected behavior on distroless
func filterUnknownBinaryErrors(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "executable file not found in $PATH") {
		return nil
	}
	return err
}

// getProxyLogs fetches proxy logs for the given namespace/pod/container and stores the output in global structs.
// Runs if a goroutine, with errors reported through gErrors.
// TODO(stewartbutler): output the logs to a more robust/complete structure.
func getProxyLogs(runner *kubectlcmd.Runner, config *config.BugReportConfig, resources *cluster2.Resources,
	path, namespace, pod, container string, wg *sync.WaitGroup,
) {
	startTime := time.Now()
	wg.Add(1)
	log.Infof("Waiting on proxy logs %v/%v/%v", namespace, pod, container)
	go func() {
		defer func() {
			wg.Done()
			logRuntime(startTime, "Done getting from proxy logs for %v/%v/%v", namespace, pod, container)
		}()

		clog, cstat, imp, err := getLog(runner, resources, config, namespace, pod, container)
		appendGlobalErr(err)
		lock.Lock()
		if err == nil {
			logs[path], stats[path], importance[path] = clog, cstat, imp
		}
		lock.Unlock()
		log.Infof("Done with proxy logs %v/%v/%v", namespace, pod, container)
	}()
}

// getIstiodLogs fetches Istiod logs for the given namespace/pod and writes the output.
// Runs if a goroutine, with errors reported through gErrors.
func getIstiodLogs(runner *kubectlcmd.Runner, config *config.BugReportConfig, resources *cluster2.Resources,
	namespace, pod string, wg *sync.WaitGroup,
) {
	startTime := time.Now()
	wg.Add(1)
	log.Infof("Waiting on Istiod logs for %v/%v", namespace, pod)
	go func() {
		defer func() {
			wg.Done()
			logRuntime(startTime, "Done getting Istiod logs for %v/%v", namespace, pod)
		}()

		clog, _, _, err := getLog(runner, resources, config, namespace, pod, common.DiscoveryContainerName)
		appendGlobalErr(err)
		writeFile(filepath.Join(archive.IstiodPath(tempDir, namespace, pod), "discovery.log"), clog, config.DryRun)
		log.Infof("Done with Istiod logs for %v/%v", namespace, pod)
	}()
}

// getOperatorLogs fetches istio-operator logs for the given namespace/pod and writes the output.
func getOperatorLogs(runner *kubectlcmd.Runner, config *config.BugReportConfig, resources *cluster2.Resources,
	namespace, pod string, wg *sync.WaitGroup,
) {
	startTime := time.Now()
	wg.Add(1)
	log.Infof("Waiting on operator logs for %v/%v", namespace, pod)
	go func() {
		defer func() {
			wg.Done()
			logRuntime(startTime, "Done getting operator logs for %v/%v", namespace, pod)
		}()

		clog, _, _, err := getLog(runner, resources, config, namespace, pod, common.OperatorContainerName)
		appendGlobalErr(err)
		writeFile(filepath.Join(archive.OperatorPath(tempDir, namespace, pod), "operator.log"), clog, config.DryRun)
		log.Infof("Done with operator logs for %v/%v", namespace, pod)
	}()
}

// getCniLogs fetches Cni logs from istio-cni-node daemonsets inside namespace kube-system and writes the output
// Runs if a goroutine, with errors reported through gErrors
func getCniLogs(runner *kubectlcmd.Runner, config *config.BugReportConfig, resources *cluster2.Resources,
	namespace, pod string, wg *sync.WaitGroup,
) {
	startTime := time.Now()
	wg.Add(1)
	log.Infof("Waiting on CNI logs for %v", pod)
	go func() {
		defer func() {
			wg.Done()
			logRuntime(startTime, "Done getting CNI logs for %v", pod)
		}()

		clog, _, _, err := getLog(runner, resources, config, namespace, pod, "")
		appendGlobalErr(err)
		writeFile(filepath.Join(archive.CniPath(tempDir, pod), "cni.log"), clog, config.DryRun)
		log.Infof("Done with CNI logs %v", pod)
	}()
}

// getLog fetches the logs for the given namespace/pod/container and returns the log text and stats for it.
func getLog(runner *kubectlcmd.Runner, resources *cluster2.Resources, config *config.BugReportConfig,
	namespace, pod, container string,
) (string, *processlog.Stats, int, error) {
	defer logRuntime(time.Now(), "Done getting logs only for %v/%v/%v", namespace, pod, container)

	log.Infof("Getting logs for %s/%s/%s...", namespace, pod, container)
	clog, err := runner.Logs(namespace, pod, container, false, config.DryRun)
	if err != nil {
		return "", nil, 0, err
	}
	if resources.ContainerRestarts(namespace, pod, container, common.IsCniPod(pod)) > 0 {
		pclog, err := runner.Logs(namespace, pod, container, true, config.DryRun)
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

func runAnalyze(config *config.BugReportConfig, params *content.Params, analyzeTimeout time.Duration) {
	newParam := params.SetNamespace(common.NamespaceAll)

	defer logRuntime(time.Now(), "Done running Istio analyze on all namespaces and report")

	common.LogAndPrintf("Running Istio analyze on all namespaces and report as below:")
	out, err := content.GetAnalyze(newParam.SetIstioNamespace(config.IstioNamespace), analyzeTimeout)
	if err != nil {
		log.Error(err.Error())
		return
	}
	common.LogAndPrintf("\nAnalysis Report:\n")
	common.LogAndPrintf("%s", out[common.StrNamespaceAll])
	common.LogAndPrintf("\n")
	writeFiles(archive.AnalyzePath(tempDir, common.StrNamespaceAll), out, config.DryRun)
}

func writeFiles(dir string, files map[string]string, dryRun bool) {
	defer logRuntime(time.Now(), "Done writing files for dir %v", dir)
	for fname, text := range files {
		writeFile(filepath.Join(dir, fname), text, dryRun)
	}
}

func writeFile(path, text string, dryRun bool) {
	if dryRun {
		return
	}
	if strings.TrimSpace(text) == "" {
		return
	}
	mkdirOrExit(path)

	defer logRuntime(time.Now(), "Done writing file for path %v", path)

	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		log.Errorf(err.Error())
	}
}

func mkdirOrExit(fpath string) {
	if err := os.MkdirAll(path.Dir(fpath), 0o755); err != nil {
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
	opt2.SetDefaultOutputLevel("default", log.InfoLevel)

	return log.Configure(&opt2)
}

func logRuntime(start time.Time, format string, args ...any) {
	log.WithLabels("runtime", time.Since(start)).Infof(format, args...)
}
