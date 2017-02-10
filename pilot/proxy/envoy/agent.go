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
	// Envoy config root
	configRoot string
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
	// EnvoyFileTemplate is a template for the root config JSON
	EnvoyFileTemplate = "%s/envoy-rev%d.json"
)

// NewAgent creates a new proxy instance agent for a config root
func NewAgent(binary string, configRoot string) Agent {
	return &agent{
		binary:     binary,
		configRoot: configRoot,
		cmdMap:     make(map[*exec.Cmd]instance),
	}
}

func configFile(config string, epoch int) string {
	return fmt.Sprintf(EnvoyFileTemplate, config, epoch)
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
	fname := configFile(s.configRoot, epoch)
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
		if err := config.Write(os.Stderr); err != nil {
			glog.Error(err)
		}
	}

	glog.V(2).Infof("Envoy starting: %v", args)
	/* #nosec */
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
// TODO: initiate a restart if Envoy crashes by itself
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
	path := configFile(s.configRoot, epoch)
	if err := os.Remove(path); err != nil {
		glog.Warningf("Failed to delete config file %s, %v", path, err)
	}
	delete(s.cmdMap, cmd)
}
