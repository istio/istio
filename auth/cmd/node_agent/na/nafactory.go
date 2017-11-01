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

package na

import (
	"fmt"

	"github.com/golang/glog"

	"istio.io/istio/auth/pkg/platform"
	"istio.io/istio/auth/pkg/workload"
)

// NodeAgent interface that should be implemented by
// various platform specific node agents.
type NodeAgent interface {
	Start() error
}

// NewNodeAgent is constructor for Node agent based on the provided Environment variable.
func NewNodeAgent(cfg *Config) (NodeAgent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("Nil configuration passed")
	}
	na := &nodeAgentInternal{
		config:   cfg,
		certUtil: CertUtilImpl{},
	}

	if pc, err := platform.NewClient(cfg.Env, cfg.PlatformConfig, cfg.IstioCAAddress); err == nil {
		na.pc = pc
	} else {
		return nil, err
	}

	cAClient := &cAGrpcClientImpl{}
	na.cAClient = cAClient

	// TODO: Specify files for service identity cert/key instead of node agent files.
	secretServer, err := workload.NewSecretServer(
		workload.NewSecretFileServerConfig(cfg.PlatformConfig.CertChainFile, cfg.PlatformConfig.KeyFile))
	if err != nil {
		glog.Fatalf("Workload IO creation error: %v", err)
	}
	na.secretServer = secretServer
	return na, nil
}
