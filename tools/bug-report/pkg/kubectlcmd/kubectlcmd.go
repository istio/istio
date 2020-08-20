/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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

func Logs(namespace, pod, container string, dryRun bool) (string, error) {
	stdout, stderr, err := Run([]string{"logs"},
		&Options{
			Namespace: namespace,
			ExtraArgs: []string{pod, container},
			DryRun:    dryRun,
		})
	if err != nil {
		return "", fmt.Errorf("kubectl error: %s\n\nstderr:\n%s\n\nstdout:\n%s\n\n", err, stderr, stdout)
	}
	return stdout, nil
}

// Run runs the kubectl command by specifying subcommands in subcmds with opts.
func Run(subcmds []string, opts *Options) (string, string, error) {
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

	cmd := exec.Command("Run", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	cmdStr := strings.Join(args, " ")

	if opts.DryRun {
		log.Infof("dry run mode: would be running this cmd:\nkubectl %s\n", cmdStr)
		return "", "", nil
	}

	log.Infof("running command: %s", cmdStr)
	err := cmd.Run()
	csError := util.ConsolidateLog(stderr.String())

	if err != nil {
		return stdout.String(), csError, fmt.Errorf("error running Run: %s", err)
	}

	return stdout.String(), csError, nil
}
