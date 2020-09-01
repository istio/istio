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
	"time"

	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
)

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
	return client.PodLogs(context.TODO(), pod, namespace, container, previous)
}

// Cat runs the cat command for the given path in the given namespace/pod/container.
func Cat(client kube.ExtendedClient, namespace, pod, container, path string, dryRun bool) (string, error) {
	cmdStr := "cat " + path
	if dryRun {
		return fmt.Sprintf("Dry run: would be running podExec %s/%s/%s:%s", pod, namespace, container, cmdStr), nil
	}
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
	log.Infof("podExec %s/%s/%s:%s", namespace, pod, container, cmdStr)
	stdout, stderr, err := client.PodExec(pod, namespace, container, cmdStr)
	if err != nil {
		return "", fmt.Errorf("podExec error: %s\n\nstderr:\n%s\n\nstdout:\n%s",
			err, util.ConsolidateLog(stderr), stdout)
	}
	return stdout, nil
}

// RunCmd runs the given command in kubectl, adding -n namespace if namespace is not empty.
func RunCmd(command, namespace string, dryRun bool) (string, error) {
	return Run(strings.Split(command, " "),
		&Options{
			Namespace: namespace,
			DryRun:    dryRun,
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

	log.Infof("running command: kubectl %s", cmdStr)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("kubectl error: %s\n\nstderr:\n%s\n\nstdout:\n%s",
			err, util.ConsolidateLog(stderr.String()), stdout.String())
	}

	return stdout.String(), nil
}
