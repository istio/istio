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

package secrets

import (
	"fmt"

	"istio.io/istio/security/pkg/pki/util"
)

const (
	// SecretFile the key/cert to the workload through file.
	SecretFile SecretServerMode = iota // 0
	// SecretDiscoveryServiceAPI the key/cert to the workload through SDS API.
	SecretDiscoveryServiceAPI // 1
)

// SecretServerMode is the mode SecretServer runs.
type SecretServerMode int

// SecretServer is for implementing the communication from the node agent to the workload.
type SecretServer interface {
	// Put stores the key cert bundle with associated workload identity.
	Put(serviceAccount string, bundle util.KeyCertBundle) error
}

// Config contains the SecretServer configuration.
type Config struct {
	// Mode specifies how the node agent communications to workload.
	Mode SecretServerMode

	// SecretDirectory specifies the root directory storing the key cert files, only for file mode.
	SecretDirectory string
}

// NewSecretServer instantiates a SecretServer according to the configuration.
func NewSecretServer(cfg *Config) (SecretServer, error) {
	switch cfg.Mode {
	case SecretFile:
		return &SecretFileServer{cfg.SecretDirectory}, nil
	default:
		return nil, fmt.Errorf("mode: %d is not supported", cfg.Mode)
	}
}
