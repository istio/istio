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

	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/tools/bug-report/pkg/common"
)

const (
	// The default number of in-flight requests allowed for the runner.
	defaultActiveRequestLimit = 32

	// reportInterval controls how frequently to output progress reports on running tasks.
	reportInterval = 30 * time.Second
)

type Runner struct {
	Client kube.CLIClient

	// Used to limit the number of concurrent tasks.
	taskSem chan struct{}

	// runningTasks tracks the in-flight fetch operations for user feedback.
	runningTasks   sets.String
	runningTasksMu sync.RWMutex

	// runningTasksTicker is the report interval for running tasks.
	runningTasksTicker *time.Ticker
}

func NewRunner(activeRqLimit int) *Runner {
	if activeRqLimit <= 0 {
		activeRqLimit = defaultActiveRequestLimit
	}
	return &Runner{
		taskSem:            make(chan struct{}, activeRqLimit),
		runningTasks:       sets.New[string](),
		runningTasksMu:     sync.RWMutex{},
		runningTasksTicker: time.NewTicker(reportInterval),
	}
}

func (r *Runner) SetClient(client kube.CLIClient) {
	r.Client = client
}

func (r *Runner) ReportRunningTasks() {
	go func() {
		time.Sleep(reportInterval)
		for range r.runningTasksTicker.C {
			r.printRunningTasks()
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
func (r *Runner) Logs(namespace, pod, container string, previous, dryRun bool) (string, error) {
	if dryRun {
		return fmt.Sprintf("Dry run: would be running client.PodLogs(%s, %s, %s)", pod, namespace, container), nil
	}
	// ignore cancellation errors since this is subject to global timeout.
	task := fmt.Sprintf("PodLogs %s/%s/%s", namespace, pod, container)
	r.addRunningTask(task)
	defer r.removeRunningTask(task)
	return r.Client.PodLogs(context.TODO(), pod, namespace, container, previous)
}

// EnvoyGet sends a GET request for the URL in the Envoy container in the given namespace/pod and returns the result.
func (r *Runner) EnvoyGet(namespace, pod, url string, dryRun bool, proxyAdminPort int) (string, error) {
	if dryRun {
		return fmt.Sprintf("Dry run: would be running client.EnvoyDoWithPort(%s, %s, %s, %d)", pod, namespace, url, proxyAdminPort), nil
	}
	task := fmt.Sprintf("ProxyGet %s/%s:%s", namespace, pod, url)
	r.addRunningTask(task)
	defer r.removeRunningTask(task)
	out, err := r.Client.EnvoyDoWithPort(context.TODO(), pod, namespace, "GET", url, proxyAdminPort)
	return string(out), err
}

// Cat runs the cat command for the given path in the given namespace/pod/container.
func (r *Runner) Cat(namespace, pod, container, path string, dryRun bool) (string, error) {
	cmdStr := "cat " + path
	if dryRun {
		return fmt.Sprintf("Dry run: would be running podExec %s/%s/%s:%s", pod, namespace, container, cmdStr), nil
	}
	task := fmt.Sprintf("PodExec %s/%s/%s:%s", namespace, pod, container, cmdStr)
	r.addRunningTask(task)
	defer r.removeRunningTask(task)
	stdout, stderr, err := r.Client.PodExec(pod, namespace, container, cmdStr)
	if err != nil {
		return "", fmt.Errorf("podExec error: %s\n\nstderr:\n%s\n\nstdout:\n%s",
			err, util.ConsolidateLog(stderr), stdout)
	}
	return stdout, nil
}

// Exec runs exec for the given command in the given namespace/pod/container.
func (r *Runner) Exec(namespace, pod, container, cmdStr string, dryRun bool) (string, error) {
	if dryRun {
		return fmt.Sprintf("Dry run: would be running podExec %s/%s/%s:%s", pod, namespace, container, cmdStr), nil
	}
	task := fmt.Sprintf("PodExec %s/%s/%s:%s", namespace, pod, container, cmdStr)
	r.addRunningTask(task)
	defer r.removeRunningTask(task)
	stdout, stderr, err := r.Client.PodExec(pod, namespace, container, cmdStr)
	if err != nil {
		return "", fmt.Errorf("podExec error: %s\n\nstderr:\n%s\n\nstdout:\n%s",
			err, util.ConsolidateLog(stderr), stdout)
	}
	return stdout, nil
}

// RunCmd runs the given command in kubectl, adding -n namespace if namespace is not empty.
func (r *Runner) RunCmd(command, namespace, kubeConfig, kubeContext string, dryRun bool) (string, error) {
	return r.Run(strings.Split(command, " "),
		&Options{
			Namespace:  namespace,
			DryRun:     dryRun,
			Kubeconfig: kubeConfig,
			Context:    kubeContext,
		})
}

// Run runs the kubectl command by specifying subcommands in subcmds with opts.
func (r *Runner) Run(subcmds []string, opts *Options) (string, error) {
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

	task := fmt.Sprintf("kubectl %s", cmdStr)
	r.addRunningTask(task)
	defer r.removeRunningTask(task)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("kubectl error: %s\n\nstderr:\n%s\n\nstdout:\n%s",
			err, util.ConsolidateLog(stderr.String()), stdout.String())
	}

	return stdout.String(), nil
}

func (r *Runner) printRunningTasks() {
	r.runningTasksMu.RLock()
	defer r.runningTasksMu.RUnlock()
	if r.runningTasks.IsEmpty() {
		return
	}
	common.LogAndPrintf("The following fetches are still running: \n")
	for t := range r.runningTasks {
		common.LogAndPrintf("  %s\n", t)
	}
	common.LogAndPrintf("\n")
}

func (r *Runner) addRunningTask(task string) {
	// Limit the concurrency of running tasks.
	r.taskSem <- struct{}{}

	r.runningTasksMu.Lock()
	defer r.runningTasksMu.Unlock()
	log.Infof("STARTING %s", task)
	r.runningTasks.Insert(task)
}

func (r *Runner) removeRunningTask(task string) {
	defer func() {
		// Free up a slot for another running task.
		<-r.taskSem
	}()

	r.runningTasksMu.Lock()
	defer r.runningTasksMu.Unlock()
	log.Infof("COMPLETED %s", task)
	r.runningTasks.Delete(task)
}
