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

// Apply runs the kubectl apply with the provided manifest argument
func (c *Client) Apply(dryRun, verbose bool, namespace string, manifest string, extraArgs ...string) (string, string, error) {
	if strings.TrimSpace(manifest) == "" {
		log.Infof("Empty manifest, not applying.")
		return "", "", nil
	}

	args := []string{"apply"}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}
	args = append(args, extraArgs...)
	args = append(args, "-f", "-")

	cmd := exec.Command("kubectl", args...)
	cmd.Stdin = strings.NewReader(manifest)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	cmdStr := strings.Join(cmd.Args, " ")
	if verbose {

		cmdStr += "\n" + manifest
	} else {
		cmdStr += " <use --verbose to see manifest string> \n"
	}
	if dryRun {
		logAndPrint("Apply is in dry run mode, would be applying the following in namespace %s:\n%s\n", namespace, cmdStr)
		return "", "", nil
	}

	log.Infof("applying to namespace %s:\n%s\n", namespace, cmdStr)

	err := c.cmdSite.Run(cmd)
	if err != nil {
		logAndPrint("error running kubectl apply: %s", err)
		return stdout.String(), stderr.String(), fmt.Errorf("error from running kubectl apply: %s", err)
	}

	logAndPrint("kubectl apply success")

	return stdout.String(), stderr.String(), nil
}

func logAndPrint(v ...interface{}) {
	s := fmt.Sprintf(v[0].(string), v[1:]...)
	log.Infof(s)
	fmt.Println(s)
}
