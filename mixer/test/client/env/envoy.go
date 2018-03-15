// Copyright 2017 Istio Authors. All Rights Reserved.
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

package env

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
)

// Envoy stores data for Envoy process
type Envoy struct {
	cmd   *exec.Cmd
	ports *Ports
}

// NewEnvoy creates a new Envoy struct.
func NewEnvoy(stress, faultInject bool, v2 *V2Conf, ports *Ports) (*Envoy, error) {
	// Asssume test environment has copied latest envoy to $HOME/go/bin in bin/init.sh
	envoyPath := "envoy"
	confPath := fmt.Sprintf("/tmp/config.conf.%v", ports.AdminPort)
	log.Printf("Envoy config: in %v\n", confPath)
	if err := CreateEnvoyConf(confPath, stress, faultInject, v2, ports); err != nil {
		return nil, err
	}

	args := []string{"-c", confPath, "--base-id", strconv.Itoa(int(ports.AdminPort))}
	if stress {
		args = append(args, "--concurrency", "10")
	} else {
		args = append(args, "-l", "debug", "--concurrency", "1")
	}

	/* #nosec */
	cmd := exec.Command(envoyPath, args...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	return &Envoy{
		cmd:   cmd,
		ports: ports,
	}, nil
}

// Start starts the envoy process
func (s *Envoy) Start() error {
	err := s.cmd.Start()
	if err == nil {
		url := fmt.Sprintf("http://localhost:%v/server_info", s.ports.AdminPort)
		WaitForHTTPServer(url)
		WaitForPort(s.ports.ClientProxyPort)
		WaitForPort(s.ports.ServerProxyPort)
	}
	return err
}

// Stop stops the envoy process
func (s *Envoy) Stop() error {
	log.Printf("Kill Envoy ...\n")
	err := s.cmd.Process.Kill()
	log.Printf("Kill Envoy ... Done\n")
	return err
}
