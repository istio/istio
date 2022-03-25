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

package kubectlcmd

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/tools/bug-report/pkg/common"
	"istio.io/pkg/log"
)

const (
	// maxRequestsPerSecond is the max rate of requests to the API server.
	maxRequestsPerSecond = 10
	// maxLogFetchConcurrency is the max number of logs to fetch simultaneously.
	maxLogFetchConcurrency = 10

	// reportInterval controls how frequently to output progress reports on running tasks.
	reportInterval = 30 * time.Second
)

var (
	requestLimiter  = rate.NewLimiter(maxRequestsPerSecond, maxRequestsPerSecond)
	logFetchLimitCh = make(chan struct{}, maxLogFetchConcurrency)

	// runningTasks tracks the in-flight fetch operations for user feedback.
	runningTasks   = sets.New()
	runningTasksMu sync.RWMutex

	// runningTasksTicker is the report interval for running tasks.
	runningTasksTicker = time.NewTicker(reportInterval)
)

func ReportRunningTasks() {
	go func() {
		time.Sleep(reportInterval)
		for range runningTasksTicker.C {
			printRunningTasks()
		}
	}()
}

// Options contains the Run options.
type Options struct {
	// Path to the kubeconfig file.
	Kubeconfig string
	// ComponentName of the kubeconfig context to use.
	Context string

	// namespace - k8s namespace for Run command
	Namespace string

	// DryRun performs all steps but only logs the Run command without running it.
	DryRun bool
	// Maximum amount of time to wait for resources to be ready after install when Wait=true.
	WaitTimeout time.Duration

	// output - output mode for Run i.e. --output.
	Output string

	// extraArgs - more args to be added to the Run command, which are appended to
	// the end of the Run command.
	ExtraArgs []string
}

// Logs returns the logs for the given namespace/pod/container.
func Logs(client kube.ExtendedClient, namespace, pod, container string, previous, dryRun bool) (string, error) {
	if dryRun {
		return fmt.Sprintf("Dry run: would be running client.PodLogs(%s, %s, %s)", pod, namespace, container), nil
	}
	// ignore cancellation errors since this is subject to global timeout.
	_ = requestLimiter.Wait(context.TODO())
	logFetchLimitCh <- struct{}{}
	defer func() {
		<-logFetchLimitCh
	}()
	task := fmt.Sprintf("PodLogs %s/%s/%s", namespace, pod, container)
	addRunningTask(task)
	defer removeRunningTask(task)
	return client.PodLogs(context.TODO(), pod, namespace, container, previous)
}

// EnvoyGet sends a GET request for the URL in the Envoy container in the given namespace/pod and returns the result.
func EnvoyGet(client kube.ExtendedClient, namespace, pod, url string, dryRun bool) (string, error) {
	if dryRun {
		return fmt.Sprintf("Dry run: would be running client.EnvoyDo(%s, %s, %s)", pod, namespace, url), nil
	}
	_ = requestLimiter.Wait(context.TODO())
	task := fmt.Sprintf("ProxyGet %s/%s:%s", namespace, pod, url)
	addRunningTask(task)
	defer removeRunningTask(task)
	out, err := client.EnvoyDo(context.TODO(), pod, namespace, "GET", url)
	return string(out), err
}

// Cat runs the cat command for the given path in the given namespace/pod/container.
func Cat(client kube.ExtendedClient, namespace, pod, container, path string, dryRun bool) (string, error) {
	cmdStr := "cat " + path
	if dryRun {
		return fmt.Sprintf("Dry run: would be running podExec %s/%s/%s:%s", pod, namespace, container, cmdStr), nil
	}
	_ = requestLimiter.Wait(context.TODO())
	logFetchLimitCh <- struct{}{}
	defer func() {
		<-logFetchLimitCh
	}()
	task := fmt.Sprintf("PodExec %s/%s/%s:%s", namespace, pod, container, cmdStr)
	addRunningTask(task)
	defer removeRunningTask(task)
	stdout, stderr, err := client.PodExec(pod, namespace, container, cmdStr)
	if err != nil {
		return "", fmt.Errorf("podExec error: %s\n\nstderr:\n%s\n\nstdout:\n%s",
			err, util.ConsolidateLog(stderr), stdout)
	}
	return stdout, nil
}

// Exec runs exec for the given command in the given namespace/pod/container.
func Exec(client kube.ExtendedClient, namespace, pod, container, cmdStr string, dryRun bool) (string, error) {
	if dryRun {
		return fmt.Sprintf("Dry run: would be running podExec %s/%s/%s:%s", pod, namespace, container, cmdStr), nil
	}
	_ = requestLimiter.Wait(context.TODO())
	task := fmt.Sprintf("PodExec %s/%s/%s:%s", namespace, pod, container, cmdStr)
	addRunningTask(task)
	defer removeRunningTask(task)
	stdout, stderr, err := client.PodExec(pod, namespace, container, cmdStr)
	if err != nil {
		return "", fmt.Errorf("podExec error: %s\n\nstderr:\n%s\n\nstdout:\n%s",
			err, util.ConsolidateLog(stderr), stdout)
	}
	return stdout, nil
}

// RunCmd runs the given command in kubectl, adding -n namespace if namespace is not empty.
func RunCmd(command, namespace, kubeConfig, kubeContext string, dryRun bool) (string, error) {
	return Run(strings.Split(command, " "),
		&Options{
			Namespace:  namespace,
			DryRun:     dryRun,
			Kubeconfig: kubeConfig,
			Context:    kubeContext,
		})
}

// Run runs the kubectl command by specifying subcommands in subcmds with opts.
func Run(subcmds []string, opts *Options) (string, error) {
	args := subcmds
	if opts.Kubeconfig != "" {
		args = append(args, "--kubeconfig", opts.Kubeconfig)
	}
	if opts.Context != "" {
		args = append(args, "--context", opts.Context)
	}
	if opts.Namespace != "" {
		args = append(args, "-n", opts.Namespace)
	}
	if opts.Output != "" {
		args = append(args, "-o", opts.Output)
	}
	args = append(args, opts.ExtraArgs...)

	cmd := exec.Command("kubectl", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	cmdStr := strings.Join(args, " ")

	if opts.DryRun {
		log.Infof("dry run mode: would be running this cmd:\nkubectl %s\n", cmdStr)
		return "", nil
	}

	_ = requestLimiter.Wait(context.TODO())
	task := fmt.Sprintf("kubectl %s", cmdStr)
	addRunningTask(task)
	defer removeRunningTask(task)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("kubectl error: %s\n\nstderr:\n%s\n\nstdout:\n%s",
			err, util.ConsolidateLog(stderr.String()), stdout.String())
	}

	return stdout.String(), nil
}

func printRunningTasks() {
	runningTasksMu.RLock()
	defer runningTasksMu.RUnlock()
	if runningTasks.IsEmpty() {
		return
	}
	common.LogAndPrintf("The following fetches are still running: \n")
	for t := range runningTasks {
		common.LogAndPrintf("  %s\n", t)
	}
	common.LogAndPrintf("\n")
}

func addRunningTask(task string) {
	runningTasksMu.Lock()
	defer runningTasksMu.Unlock()
	log.Infof("STARTING %s", task)
	runningTasks.Insert(task)
}

func removeRunningTask(task string) {
	runningTasksMu.Lock()
	defer runningTasksMu.Unlock()
	log.Infof("COMPLETED %s", task)
	runningTasks.Delete(task)
}
