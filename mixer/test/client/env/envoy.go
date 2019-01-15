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

package env

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"istio.io/istio/pkg/test/env"
)

// Envoy stores data for Envoy process
type Envoy struct {
	cmd    *exec.Cmd
	ports  *Ports
	baseID string
}

// NewEnvoy creates a new Envoy struct and starts envoy.
func (s *TestSetup) NewEnvoy() (*Envoy, error) {
	confPath := filepath.Join(env.IstioOut, fmt.Sprintf("config.conf.%v.yaml", s.ports.AdminPort))
	log.Printf("Envoy config: in %v\n", confPath)
	if err := s.CreateEnvoyConf(confPath); err != nil {
		return nil, err
	}

	debugLevel := os.Getenv("ENVOY_DEBUG")
	if len(debugLevel) == 0 {
		debugLevel = "info"
	}

	baseID := ""
	args := []string{"-c", confPath,
		"--drain-time-s", "1",
		"--allow-unknown-fields"}
	if s.stress {
		args = append(args, "--concurrency", "10")
	} else {
		// debug is far too verbose.
		args = append(args, "-l", debugLevel, "--concurrency", "1")
	}
	if s.disableHotRestart {
		args = append(args, "--disable-hot-restart")
	} else {
		baseID = strconv.Itoa(int(s.testName))
		args = append(args,
			// base id is shared between restarted envoys
			"--base-id", baseID,
			"--parent-shutdown-time-s", "1",
			"--restart-epoch", strconv.Itoa(s.epoch))
	}
	if s.EnvoyParams != nil {
		args = append(args, s.EnvoyParams...)
	}
	/* #nosec */
	envoyPath := filepath.Join(env.IstioBin, "envoy")
	if path, exists := os.LookupEnv("ENVOY_PATH"); exists {
		envoyPath = path
	}
	cmd := exec.Command(envoyPath, args...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	if s.Dir != "" {
		cmd.Dir = s.Dir
	}
	return &Envoy{
		cmd:    cmd,
		ports:  s.ports,
		baseID: baseID,
	}, nil
}

// Start starts the envoy process
func (s *Envoy) Start() error {
	err := s.cmd.Start()
	if err != nil {
		return err
	}

	url := fmt.Sprintf("http://localhost:%v/server_info", s.ports.AdminPort)
	return WaitForHTTPServer(url)
}

// Stop stops the envoy process
func (s *Envoy) Stop() error {
	log.Printf("stop envoy ...\n")
	_, _, _ = HTTPPost(fmt.Sprintf("http://127.0.0.1:%v/quitquitquit", s.ports.AdminPort), "", "")
	done := make(chan error, 1)
	go func() {
		done <- s.cmd.Wait()
	}()

	select {
	case <-time.After(3 * time.Second):
		log.Println("envoy killed as timeout reached")
		if err := s.cmd.Process.Kill(); err != nil {
			return err
		}
	case err := <-done:
		log.Printf("stop envoy ... done\n")
		return err
	}

	return nil
}

// TearDown removes shared memory left by Envoy
func (s *Envoy) TearDown() {
	if s.baseID != "" {
		path := "/dev/shm/envoy_shared_memory_" + s.baseID + "0"
		if err := os.Remove(path); err != nil {
			log.Printf("failed to %s\n", err)
		} else {
			log.Printf("removed Envoy's shared memory\n")
		}
	}
}
