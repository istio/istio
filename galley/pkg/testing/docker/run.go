// Copyright 2018 Istio Authors
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

package docker

import (
	"bytes"
	"errors"
	"os/exec"
)

// RunArgs are arguments to supply to "docker run".
type RunArgs struct {
	Name    string
	Mount   string
	Expose  string
	Publish string
	Image   string
}

// Instance of a running "docker run" process.
type Instance struct {
	cmd *exec.Cmd
}

// Start a "docker run" process, with the given args.
func Start(a RunArgs) (*Instance, error) {

	if a.Image == "" {
		return nil, errors.New("args.Image cannot be empty")
	}

	args := []string{"run"}

	if a.Name != "" {
		args = append(args, "--name="+a.Name)
	}

	if a.Mount != "" {
		args = append(args, "--mount="+a.Mount)
	}

	if a.Expose != "" {
		args = append(args, "--expose="+a.Expose)
	}

	if a.Publish != "" {
		args = append(args, "--publish="+a.Publish)
	}

	args = append(args, a.Image)

	c := exec.Command("docker", args...)

	var b bytes.Buffer
	c.Stdout = &b
	c.Stderr = &b

	if err := c.Start(); err != nil {
		return nil, err
	}

	return &Instance{c}, nil
}

// Kill a running "docker run" process.
func (i *Instance) Kill() (out string, err error) {
	err = i.cmd.Process.Kill()
	i.cmd.Process.Wait()

	out = (i.cmd.Stderr).(*bytes.Buffer).String()
	return
}
