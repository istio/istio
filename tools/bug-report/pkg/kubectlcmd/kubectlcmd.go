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
	"fmt"
	"os/exec"
	"strings"
	"time"

	"istio.io/istio/operator/pkg/util"
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
func Logs(namespace, pod, container string, previous, dryRun bool) (string, error) {
	cmdStr := []string{"logs"}
	if previous {
		cmdStr = append(cmdStr, "-p")
	}
	stdout, err := Run(cmdStr,
		&Options{
			Namespace: namespace,
			ExtraArgs: []string{pod, "-c", container},
			DryRun:    dryRun,
		})
	if err != nil {
		return "", err
	}
	return stdout, nil
}

// Exec runs exec for the given command in the given namespace/pod/container.
func Exec(namespace, pod, container, command string, dryRun bool) (string, error) {
	cmdStr := []string{"exec"}
	return Run(cmdStr,
		&Options{
			Namespace: namespace,
			ExtraArgs: []string{"-i", "-t", pod, "-c", container, "--", command},
			DryRun:    dryRun,
		})
}

// Cat runs the cat command for the given path in the given namespace/pod/container.
func Cat(namespace, pod, container, path string, dryRun bool) (string, error) {
	return Exec(namespace, pod, container, `cat `+path, dryRun)
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
		return "", fmt.Errorf("kubectl error: %s\n\nstderr:\n%s\n\nstdout:\n%s\n\n",
			err, util.ConsolidateLog(stderr.String()), stdout.String())
	}

	return stdout.String(), nil
}
