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

	"istio.io/operator/pkg/util"
	"istio.io/pkg/log"
)

// New creates a Client that runs kubectl available on the path with default authentication
func New() *Client {
	return &Client{cmdSite: &console{}}
}

// Client provides an interface to kubectl
type Client struct {
	cmdSite commandSite
}

// Options contains the startup options for applying the manifest.
type Options struct {
	// Path to the kubeconfig file.
	Kubeconfig string
	// ComponentName of the kubeconfig context to use.
	Context string

	// namespace - k8s namespace for kubectl command
	Namespace string

	// DryRun performs all steps except actually applying the manifests or creating output dirs/files.
	DryRun bool
	// Verbose enables verbose debug output.
	Verbose bool
	// Wait for resources to be ready after install.
	Wait bool
	// Prune controls whether to pass --prune to kubectl. Unset means don't care (internal logic is free to modify).
	Prune *bool
	// Maximum amount of time to wait for resources to be ready after install when Wait=true.
	WaitTimeout time.Duration

	// stdin - cmd stdin input as string
	Stdin string
	// output - output mode for kubectl, i.e., -o, --output
	Output string

	// extraArgs - more args to be added to the kubectl command
	ExtraArgs []string
}

// Apply runs the `kubectl apply` command on the passed in manifest string with the given options.
// It returns stdout, stderr from the `kubectl` command as strings, and error for errors external to kubectl.
func (c *Client) Apply(manifest string, opts *Options) (string, string, error) {
	if strings.TrimSpace(manifest) == "" {
		log.Infof("Empty manifest, not running kubectl apply.")
		return "", "", nil
	}
	subcmds := []string{"apply"}
	opts.Stdin = manifest
	return c.kubectl(subcmds, opts)
}

// Delete runs the `kubectl delete` command on the passed in manifest string with the given options.
// It returns stdout, stderr from the `kubectl` command as strings, and error for errors external to kubectl.
func (c *Client) Delete(manifest string, opts *Options) (string, string, error) {
	if strings.TrimSpace(manifest) == "" {
		log.Infof("Empty manifest, not running kubectl delete.")
		return "", "", nil
	}
	subcmds := []string{"delete"}
	opts.Stdin = manifest
	return c.kubectl(subcmds, opts)
}

// GetAll runs the `kubectl get all` with the given options.
// It returns stdout, stderr from the `kubectl` command as strings, and error for errors external to kubectl.
func (c *Client) GetAll(opts *Options) (string, string, error) {
	return c.kubectl([]string{"get", "all"}, opts)
}

// GetConfigMap runs the `kubectl get cm` command with the given options.
// name - name of the config map to get
// It returns stdout, stderr from the `kubectl` command as strings, and error for errors external to kubectl.
func (c *Client) GetConfigMap(name string, opts *Options) (string, string, error) {
	return c.kubectl([]string{"get", "cm", name}, opts)
}

// kubectl runs the `kubectl` command by specifying subcommands in subcmds with opts.
func (c *Client) kubectl(subcmds []string, opts *Options) (string, string, error) {
	hasStdin := strings.TrimSpace(opts.Stdin) != ""
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
	if opts.Prune != nil && *opts.Prune {
		args = append(args, "--prune")
	}
	args = append(args, opts.ExtraArgs...)

	if hasStdin {
		args = append(args, "-f", "-")
	}

	cmd := exec.Command("kubectl", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	cmdStr := strings.Join(args, " ")
	if hasStdin {
		cmd.Stdin = strings.NewReader(opts.Stdin)
		if opts.Verbose {
			cmdStr += "\n" + opts.Stdin
		} else {
			cmdStr += " <use --verbose to see stdin string> \n"
		}
	}

	if opts.DryRun {
		log.Infof("dry run mode: would be running this cmd:\n%s\n", cmdStr)
		return "", "", nil
	}

	log.Infof("running command:\n%s\n", cmdStr)
	err := c.cmdSite.Run(cmd)
	csError := util.ConsolidateLog(stderr.String())

	if err != nil {
		log.Errorf("error running kubectl: %s", err)
		return stdout.String(), csError, fmt.Errorf("error running kubectl: %s", err)
	}

	log.Infof("command succeeded: %s", cmdStr)

	return stdout.String(), csError, nil
}

// commandSite allows for tests to mock cmd.Run() events
type commandSite interface {
	Run(*exec.Cmd) error
}
type console struct {
}

func (console) Run(c *exec.Cmd) error {
	return c.Run()
}
