// Copyright 2017 Google Inc.
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

package envoy

import (
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/golang/glog"
)

// Agent manages a proxy instance
type Agent interface {
	Reload(config *Config) error
	ActiveConfig() *Config
}

type agent struct {
	// Envoy binary path
	binary string
	// Map of known running Envoy processes and their restart epochs.
	cmdMap map[*exec.Cmd]instance
	// mutex protects cmdMap
	mutex sync.Mutex
}

type instance struct {
	epoch  int
	config *Config
}

const (
	EnvoyConfigPath   = "/etc/envoy/"
	EnvoyFileTemplate = "envoy-rev%d.json"
)

// NewAgent creates a new instance.
func NewAgent(binary string, mixer string) (Agent, error) {
	// TODO mixer should be configured directly in the envoy filter
	f, err := os.Create(EnvoyConfigPath + ServerConfig)
	if err != nil {
		return nil, err
	}
	f.WriteString(fmt.Sprintf(`
cloud_tracing_config {
  force_disable: true
}
mixer_options {
  mixer_server: "%s%s"
}
	`, OutboundClusterPrefix, mixer))
	f.Close()

	return &agent{
		binary: binary,
		cmdMap: make(map[*exec.Cmd]instance),
	}, nil
}

func configFile(epoch int) string {
	return fmt.Sprintf(EnvoyConfigPath+EnvoyFileTemplate, epoch)
}

// Reload Envoy with a hot restart. Envoy hot restarts are performed by launching a new Envoy process with an
// incremented restart epoch. To successfully launch a new Envoy process that will replace the running Envoy processes,
// the restart epoch of the new process must be exactly 1 greater than the highest restart epoch of the currently
// running Envoy processes. To ensure that we launch the new Envoy process with the correct restart epoch, we keep track
// of all running Envoy processes and their restart epochs.
//
// Envoy hot restart documentation: https://lyft.github.io/envoy/docs/intro/arch_overview/hot_restart.html
func (s *agent) Reload(config *Config) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Discover the latest running instance
	epoch := -1
	for _, running := range s.cmdMap {
		if running.epoch > epoch {
			epoch = running.epoch
		}
	}
	epoch++

	// Write config file
	fname := configFile(epoch)
	if err := config.WriteFile(fname); err != nil {
		return err
	}

	// Spin up a new Envoy process
	args := []string{"-c", fname, "--restart-epoch", fmt.Sprint(epoch),
		"--drain-time-s", "60", "--parent-shutdown-time-s", "90"}
	if glog.V(3) {
		args = append(args, "-l", "debug")
	}
	if glog.V(2) {
		config.Write(os.Stderr)
	}

	glog.V(2).Infof("Envoy starting: %v", args)
	cmd := exec.Command(s.binary, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	s.cmdMap[cmd] = instance{
		epoch:  epoch,
		config: config,
	}

	// Start tracking the process.
	go s.waitForExit(cmd)

	return nil
}

// ActiveConfig returns the config for the instance with the highest epoch
func (s *agent) ActiveConfig() (config *Config) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	epoch := -1
	for _, running := range s.cmdMap {
		if running.epoch > epoch {
			epoch = running.epoch
			config = running.config
		}
	}

	return
}

// waitForExit waits until the command exits and removes it from the set of known running Envoy processes.
func (s *agent) waitForExit(cmd *exec.Cmd) {
	err := cmd.Wait()

	s.mutex.Lock()
	defer s.mutex.Unlock()

	epoch := s.cmdMap[cmd].epoch
	if err != nil {
		glog.V(2).Infof("Envoy epoch %d terminated: %v", epoch, err)
	} else {
		glog.V(2).Infof("Envoy epoch %d process exited", epoch)
	}

	// delete config file
	path := configFile(epoch)
	if err := os.Remove(path); err != nil {
		glog.Warningf("Failed to delete config file %s, %v", path, err)
	}
	delete(s.cmdMap, cmd)
}
