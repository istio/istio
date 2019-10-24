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

// commandSite allows for tests to mock cmd.Run() events
type commandSite interface {
	Run(*exec.Cmd) error
}
type console struct {
}

func (console) Run(c *exec.Cmd) error {
	return c.Run()
}

// kubectlParams is a set of params passed to kubectl.
type kubectlParams struct {
	// dryRun - display the command but don't run it
	dryRun bool
	// verbose - dump the full manifest
	verbose bool
	// kubeconfig - the path to the kube config
	kubeconfig string
	// context - used to identify the cluster
	context string
	// namespace - k8s namespace for kubectl command
	namespace string
	// stdin - cmd stdin input as string
	stdin string
	// output - output mode for kubectl, i.e., -o, --output
	output string
	// extraArgs - more args to be added to the kubectl command
	extraArgs []string
}

// Apply runs the `kubectl apply` command with parameters:
// dryRun - display the command but don't run it
// verbose - dump the full manifest
// kubeconfig, context - used to identify the cluster
// namespace - k8s namespace for kubectl command
// manifest - manifests to be applied to the cluster
// extraArgs - more args to be added to the kubectl command
//
// It returns stdout, stderr from the `kubectl` command as strings, and error for errors external to kubectl.
func (c *Client) Apply(dryRun, verbose bool, kubeconfig, context, namespace string,
	manifest string, extraArgs ...string) (string, string, error) {
	if strings.TrimSpace(manifest) == "" {
		log.Infof("Empty manifest, not running kubectl apply.")
		return "", "", nil
	}
	subcmds := []string{"apply"}
	params := &kubectlParams{
		dryRun:     dryRun,
		verbose:    verbose,
		kubeconfig: kubeconfig,
		context:    context,
		namespace:  namespace,
		stdin:      manifest,
		output:     "",
		extraArgs:  extraArgs,
	}
	return c.kubectl(subcmds, params)
}

// Delete runs the `kubectl delete` command with the following parameters:
// dryRun - display the command but don't run it
// verbose - dump the full manifest
// kubeconfig, context - used to identify the cluster
// namespace - k8s namespace for kubectl command
// manifest - manifests of resources to be deleted in the cluster
// extraArgs - more args to be added to the kubectl command
//
// It returns stdout, stderr from the `kubectl` command as strings, and error for errors external to kubectl.
func (c *Client) Delete(dryRun, verbose bool, kubeconfig, context, namespace string,
	manifest string, extraArgs ...string) (string, string, error) {
	if strings.TrimSpace(manifest) == "" {
		log.Infof("Empty manifest, not running kubectl delete.")
		return "", "", nil
	}
	subcmds := []string{"delete"}
	params := &kubectlParams{
		dryRun:     dryRun,
		verbose:    verbose,
		kubeconfig: kubeconfig,
		context:    context,
		namespace:  namespace,
		stdin:      manifest,
		output:     "",
		extraArgs:  extraArgs,
	}
	return c.kubectl(subcmds, params)
}

// GetAll runs the `kubectl get all` with with parameters:
// kubeconfig, context - used to identify the cluster
// namespace - k8s namespace for kubectl command
// output - output mode for kubectl
// extraArgs - more args to be added to the kubectl command
//
// It returns stdout, stderr from the `kubectl` command as strings, and error for errors external to kubectl.
func (c *Client) GetAll(kubeconfig, context, namespace, output string,
	extraArgs ...string) (string, string, error) {
	subcmds := []string{"get", "all"}
	params := &kubectlParams{
		dryRun:     false,
		verbose:    false,
		kubeconfig: kubeconfig,
		context:    context,
		namespace:  namespace,
		stdin:      "",
		output:     output,
		extraArgs:  extraArgs,
	}
	return c.kubectl(subcmds, params)
}

// GetConfig runs the `kubectl get cm` command with parameters:
// kubeconfig, context - used to identify the cluster
// name - name of the config map to get
// namespace - k8s namespace for kubectl command
// output - output mode for kubectl
// extraArgs - more args to be added to the kubectl command
//
// It returns stdout, stderr from the `kubectl` command as strings, and error for errors external to kubectl.
func (c *Client) GetConfig(kubeconfig, context, name, namespace, output string,
	extraArgs ...string) (string, string, error) {
	subcmds := []string{"get", "cm", name}
	params := &kubectlParams{
		dryRun:     false,
		verbose:    false,
		kubeconfig: kubeconfig,
		context:    context,
		namespace:  namespace,
		stdin:      "",
		output:     output,
		extraArgs:  extraArgs,
	}
	return c.kubectl(subcmds, params)
}

func logAndPrint(v ...interface{}) {
	s := fmt.Sprintf(v[0].(string), v[1:]...)
	log.Infof(s)
	fmt.Println(s)
}

// kubectl runs the `kubectl` command by specifying subcommands in subcmds with kubectlParams
func (c *Client) kubectl(subcmds []string, params *kubectlParams) (string, string, error) {
	hasStdin := strings.TrimSpace(params.stdin) != ""
	args := subcmds
	if params.kubeconfig != "" {
		args = append(args, "--kubeconfig", params.kubeconfig)
	}
	if params.context != "" {
		args = append(args, "--context", params.context)
	}
	if params.namespace != "" {
		args = append(args, "-n", params.namespace)
	}
	if params.output != "" {
		args = append(args, "-o", params.output)
	}
	args = append(args, params.extraArgs...)

	if hasStdin {
		args = append(args, "-f", "-")
	}

	cmd := exec.Command("kubectl", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	cmdStr := strings.Join(args, " ")
	if hasStdin {
		cmd.Stdin = strings.NewReader(params.stdin)
		if params.verbose {
			cmdStr += "\n" + params.stdin
		} else {
			cmdStr += " <use --verbose to see stdin string> \n"
		}
	}

	if params.dryRun {
		logAndPrint("dry run mode: would be running this cmd:\n%s\n", cmdStr)
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
