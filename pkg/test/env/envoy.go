// Copyright Istio Authors
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
	"context"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"istio.io/istio/pkg/envoy"
	"istio.io/pkg/env"
)

const (
	liveTimeout = 10 * time.Second
	waitTimeout = 3 * time.Second
)

// newEnvoy creates a new Envoy struct and starts envoy.
func (s *TestSetup) newEnvoy() (envoy.Instance, error) {
	confPath := filepath.Join(IstioOut, fmt.Sprintf("config.conf.%v.yaml", s.ports.AdminPort))
	log.Printf("Envoy config: in %v\n", confPath)
	if err := s.CreateEnvoyConf(confPath); err != nil {
		return nil, err
	}

	debugLevel := env.RegisterStringVar("ENVOY_DEBUG", "info", "Specifies the debug level for Envoy.").Get()

	options := []envoy.Option{
		envoy.ConfigPath(confPath),
		envoy.DrainDuration(1 * time.Second),
	}
	if s.stress {
		options = append(options, envoy.Concurrency(10))
	} else {
		// debug is far too verbose.
		options = append(options,
			envoy.LogLevel(debugLevel),
			envoy.Concurrency(1))
	}
	if s.disableHotRestart {
		options = append(options, envoy.DisableHotRestart(true))
	} else {
		options = append(options,
			envoy.BaseID(uint32(s.testName)),
			envoy.ParentShutdownDuration(1*time.Second),
			envoy.Epoch(s.epoch))
	}
	if s.EnvoyParams != nil {
		o, err := envoy.NewOptions(s.EnvoyParams...)
		if err != nil {
			return nil, err
		}
		options = append(options, o...)
	}
	/* #nosec */
	// Since we are possible running in a container, the OS may be different that what we are building (we build for host OS),
	// we need to use the local container's OS bin found in LOCAL_OUT
	envoyPath := filepath.Join(LocalOut, "envoy")
	if path, exists := env.RegisterStringVar("ENVOY_PATH", "", "Specifies the path to an Envoy binary.").Lookup(); exists {
		envoyPath = path
	}
	i, err := envoy.New(envoy.Config{
		Name:            fmt.Sprintf("envoy-%d", uint32(s.testName)),
		AdminPort:       uint32(s.ports.AdminPort),
		BinaryPath:      envoyPath,
		WorkingDir:      s.Dir,
		SkipBaseIDClose: true,
		Options:         options,
	})
	if err != nil {
		return nil, err
	}
	return i, nil
}

// startEnvoy starts the envoy process
func startEnvoy(e envoy.Instance) error {
	return e.Start(context.Background()).WaitLive().WithTimeout(liveTimeout).Do()
}

// stopEnvoy stops the envoy process
func stopEnvoy(e envoy.Instance) error {
	log.Printf("stop envoy ...\n")
	if e == nil {
		return nil
	}
	err := e.ShutdownAndWait().WithTimeout(waitTimeout).Do()
	if err == context.DeadlineExceeded {
		return e.KillAndWait().WithTimeout(waitTimeout).Do()
	}
	return err
}

// removeEnvoySharedMemory removes shared memory left by Envoy
func removeEnvoySharedMemory(e envoy.Instance) {
	if err := e.BaseID().Close(); err != nil {
		log.Printf("failed to remove Envoy's shared memory: %s\n", err)
	} else {
		log.Printf("removed Envoy's shared memory\n")
	}
}
