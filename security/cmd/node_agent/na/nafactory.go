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
	"os"

	"cloud.google.com/go/compute/metadata"

	"istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/caclient/grpc"
	"istio.io/istio/security/pkg/platform"
	"istio.io/istio/security/pkg/util"
	"istio.io/istio/security/pkg/workload"
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
		certUtil: util.NewCertUtil(cfg.CSRGracePeriodPercentage),
	}

	env := determinePlatform(cfg)
	pc, err := platform.NewClient(env, cfg.RootCertFile, cfg.KeyFile,
		cfg.CertChainFile, cfg.IstioCAAddress)
	if err != nil {
		return nil, err
	}
	na.pc = pc

	cAClient := &grpc.CAGrpcClientImpl{}
	na.cAClient = cAClient

	// TODO: Specify files for service identity cert/key instead of node agent files.
	secretServer, err := workload.NewSecretServer(
		workload.NewSecretFileServerConfig(cfg.CertChainFile, cfg.KeyFile))
	if err != nil {
		log.Errorf("Workload IO creation error: %v", err)
		os.Exit(-1)
	}
	na.secretServer = secretServer
	return na, nil
}

// determinePlatform choose the right platform. If the env is specified in cfg.Env,
// then we will use it. Otherwise nodeagent will detect the platform for you.
func determinePlatform(cfg *Config) string {
	if cfg.Env != "unspecified" {
		return cfg.Env
	}
	if metadata.OnGCE() {
		return "gcp"
	}
	return "onprem"
}
