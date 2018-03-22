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

package caclient

import (
	"fmt"

	"istio.io/istio/security/pkg/caclient/grpc"
	pkiutil "istio.io/istio/security/pkg/pki/util"
	"istio.io/istio/security/pkg/platform"
	"istio.io/istio/security/pkg/util"
)

// NewKeyCertBundleRotator is constructor for keyCertBundleRotatorImpl based on the provided configuration.
func NewKeyCertBundleRotator(cfg *Config, bundle pkiutil.KeyCertBundle) (KeyCertBundleRotator, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil configuration passed")
	}

	pc, err := platform.NewClient(cfg.Env, cfg.RootCertFile, cfg.KeyFile, cfg.CertChainFile, cfg.CAAddress)
	if err != nil {
		return nil, err
	}
	cAClient, err := NewCAClient(pc, &grpc.CAGrpcClientImpl{}, cfg.CAAddress, cfg.Org, cfg.RSAKeySize,
		cfg.RequestedCertTTL, cfg.ForCA, cfg.CSRMaxRetries, cfg.CSRInitialRetrialInterval)
	if err != nil {
		return nil, fmt.Errorf("Failed to initialize CAClient: %v", err)
	}

	if err != nil {
		return nil, fmt.Errorf("Failed to initialize KeyCertBundle: %v", err)
	}

	return &keyCertBundleRotatorImpl{
		certUtil:  util.NewCertUtil(cfg.CSRGracePeriodPercentage),
		retriever: cAClient,
		keycert:   bundle,
		stopCh:    make(chan bool, 1),
		stopped:   true,
	}, nil
}
