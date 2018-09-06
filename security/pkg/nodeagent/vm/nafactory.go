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

package vm

import (
	"fmt"

	"istio.io/istio/security/pkg/caclient/protocol"
	"istio.io/istio/security/pkg/platform"
	"istio.io/istio/security/pkg/util"
)

// NodeAgent interface that should be implemented by
// various platform specific node agents.
type NodeAgent interface {
	Start() error
}

// NewNodeAgent is constructor for Node agent based on the provided Environment variable.
func NewNodeAgent(cfg *Config) (NodeAgent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil configuration passed")
	}
	na := &nodeAgentInternal{
		config:   cfg,
		certUtil: util.NewCertUtil(cfg.CAClientConfig.CSRGracePeriodPercentage),
	}

	pc, err := platform.NewClient(cfg.CAClientConfig.Env, cfg.CAClientConfig.RootCertFile, cfg.CAClientConfig.KeyFile,
		cfg.CAClientConfig.CertChainFile, cfg.CAClientConfig.CAAddress)
	if err != nil {
		return nil, err
	}
	na.pc = pc
	dialOpts, err := pc.GetDialOptions()
	if err != nil {
		return nil, err
	}
	grpcConn, err := protocol.NewGrpcConnection(cfg.CAClientConfig.CAAddress, dialOpts)
	if err != nil {
		return nil, err
	}
	na.caProtocol = grpcConn
	return na, nil
}
