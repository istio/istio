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
	"fmt"
	"strings"

	"istio.io/istio/pilot/test/util"
)

var shell = util.Shell

func dockerErr(op string, out string, err error) error {
	return fmt.Errorf("[docker/%s] err:'%v', out: '%s'", op, out, err)
}

// Pull executes "docker pull <imageName>"
func Pull(imageName string) error {
	if s, err := shell(fmt.Sprintf("docker pull %s", imageName)); err != nil {
		return dockerErr("pull", s, err)
	}

	return nil
}

// Kill executes "docker kill <containerName>"
func Kill(containerName string) error {
	if s, err := shell(fmt.Sprintf("docker kill %s", containerName)); err != nil {
		return dockerErr("pull", s, err)
	}

	return nil
}

// Port executes "docker port <containerName>"
func Port(containerName string) (s string, err error) {
	if s, err = shell(fmt.Sprintf("docker port %s", containerName)); err != nil {
		s = ""
		err = dockerErr("port", s, err)
	}
	return
}

// GetExternalPort finds the matching external port for the given internal port.
func GetExternalPort(containerName string, internalPort string) (string, error) {
	ports, err := Port(containerName)
	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(ports, "\n") {
		parts := strings.Split(line, "->")
		if len(parts) != 2 {
			return "", fmt.Errorf("unrecognized port line: '%s'", line)
		}

		if strings.Contains(parts[0], internalPort) {
			idx := strings.LastIndex(parts[1], ":")
			if idx == -1 {
				return "", fmt.Errorf("unrecognized port line: '%s'", line)
			}
			return parts[1][idx+1:], nil
		}
	}

	return "", fmt.Errorf("unable to find the port: internal:'%s', ports:'%s'", internalPort, ports)
}
