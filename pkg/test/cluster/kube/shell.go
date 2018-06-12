//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package kube

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"istio.io/istio/pkg/log"
)

func sh(ctx context.Context, format string, logCommand, logOutput, logError bool, args ...interface{}) (string, error) {
	command := fmt.Sprintf(format, args...)
	if logCommand {
		log.Infof("Running command %s", command)
	}
	c := exec.CommandContext(ctx, "sh", "-c", command) // #nosec
	bytes, err := c.CombinedOutput()
	if logOutput {
		if output := strings.TrimSuffix(string(bytes), "\n"); len(output) > 0 {
			log.Infof("Command output: \n%s", output)
		}
	}

	if err != nil {
		if logError {
			log.Infof("Command error: %v", err)
		}
		return string(bytes), fmt.Errorf("command failed: %q %v", string(bytes), err)
	}
	return string(bytes), nil
}

func shNoStdout(format string, args ...interface{}) (string, error) {
	return sh(context.Background(), format, true, false, true, args...)
}
