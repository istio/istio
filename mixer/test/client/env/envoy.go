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
	"path/filepath"
	"strconv"

	"istio.io/istio/tests/util"
)

// Envoy stores data for Envoy process
type Envoy struct {
	cmd   *exec.Cmd
	ports *Ports
}

// NewEnvoy creates a new Envoy struct and starts envoy.
func (s *TestSetup) NewEnvoy(stress, faultInject bool, mfConf *MixerFilterConf, ports *Ports, epoch int,
	confVersion string) (*Envoy, error) {
	confPath := filepath.Join(util.IstioOut, fmt.Sprintf("config.conf.%v.json", ports.AdminPort))
	log.Printf("Envoy config: in %v\n", confPath)
	if err := s.CreateEnvoyConf(confPath, stress, faultInject, mfConf, ports, confVersion); err != nil {
		return nil, err
	}

	debugLevel := os.Getenv("ENVOY_DEBUG")
	if len(debugLevel) == 0 {
		debugLevel = "info"
	}

	// Don't use hot-start, each Envoy re-start use different base-id
	args := []string{"-c", confPath,
		"--base-id", strconv.Itoa(int(ports.AdminPort) + epoch)}
	if stress {
		args = append(args, "--concurrency", "10")
	} else {
		// debug is far too verbose.
		args = append(args, "-l", debugLevel, "--concurrency", "1")
	}
	if s.EnvoyParams != nil {
		args = append(args, s.EnvoyParams...)
	}
	/* #nosec */
	envoyPath := filepath.Join(util.IstioBin, "envoy")
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
	if err != nil {
		return err
	}

	url := fmt.Sprintf("http://localhost:%v/server_info", s.ports.AdminPort)
	WaitForHTTPServer(url)
	WaitForPort(s.ports.ClientProxyPort)
	WaitForPort(s.ports.ServerProxyPort)

	return nil
}

// Stop stops the envoy process
func (s *Envoy) Stop() error {
	log.Printf("Kill Envoy ...\n")
	err := s.cmd.Process.Kill()
	log.Printf("Kill Envoy ... Done\n")
	return err
}
