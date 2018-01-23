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
	// TODO(nmittler): Remove this
	_ "github.com/golang/glog"

	"istio.io/istio/pkg/log"
)

// Run command and stream output
func Run(command string) error {
	log.Info(command)
	parts := strings.Split(command, " ")
	/* #nosec */
	c := exec.Command(parts[0], parts[1:]...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// RunInput command and pass input via stdin
func RunInput(command, input string) error {
	clipped := input
	if len(clipped) > 20 {
		clipped = fmt.Sprintf("%s <clipped len=%d>", clipped[0:20], len(input))
	}
	log.Infof("Run %q on input:\n%s", command, clipped)
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
	log.Info(command)
	parts := strings.Split(command, " ")
	/* #nosec */
	c := exec.Command(parts[0], parts[1:]...)
	bytes, err := c.CombinedOutput()
	if err != nil {
		log.Info(string(bytes))
		return "", fmt.Errorf("command %q failed: %q %v", command, string(bytes), err)
	}
	return string(bytes), nil
}
