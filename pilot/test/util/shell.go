// Copyright 2017 Istio Authors
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

package util

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/golang/glog"
)

// Run command and stream output
func Run(command string) error {
	glog.V(2).Info(command)
	parts := strings.Split(command, " ")
	/* #nosec */
	c := exec.Command(parts[0], parts[1:]...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// RunInput command and pass input via stdin
func RunInput(command, input string) error {
	glog.V(2).Infof("Run %q on input:\n%s", command, input)
	parts := strings.Split(command, " ")
	/* #nosec */
	c := exec.Command(parts[0], parts[1:]...)
	c.Stdin = strings.NewReader(input)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// Shell out a command and aggregate output
func Shell(command string) (string, error) {
	glog.V(2).Info(command)
	parts := strings.Split(command, " ")
	/* #nosec */
	c := exec.Command(parts[0], parts[1:]...)
	bytes, err := c.CombinedOutput()
	if err != nil {
		glog.V(2).Info(string(bytes))
		return "", fmt.Errorf("command %q failed: %q %v", command, string(bytes), err)
	}
	return string(bytes), nil
}
