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

package workload

import (
	"fmt"
)

// SecretServer is for implementing the communication from the node agent to the workload.
type SecretServer interface {
	// SetServiceIdentityPrivateKey sets the service identity private key to the channel accessible to the workload.
	SetServiceIdentityPrivateKey([]byte) error
	// SetServiceIdentityCert sets the service identity cert to the channel accessible to the workload.
	SetServiceIdentityCert([]byte) error
}

// NewSecretServer instantiates a SecretServer according to the configuration.
func NewSecretServer(cfg Config) (SecretServer, error) {
	switch cfg.Mode {
	case SecretFile:
		return &SecretFileServer{cfg}, nil
	case WorkloadAPI:
		return nil, fmt.Errorf("WORKLOAD API is unimplemented")
	default:
		return nil, fmt.Errorf("mode: %d is not supported", cfg.Mode)
	}
}
