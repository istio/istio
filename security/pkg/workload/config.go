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
	"istio.io/istio/security/pkg/util"
)

const (
	// SecretFile propages the key/cert to the workload through file.
	SecretFile int = iota // 0
	// WorkloadAPI propages the key/cert to the workload through API.
	WorkloadAPI // 1, unimplemented.
)

// Config is the configuration for node agent to workload communication.
type Config struct {
	// Mode specifies how the node agent communications to workload.
	Mode int

	// FileUtil is valid in FILE mode. It supports file I/O in a FS.
	FileUtil util.FileUtil

	// ServiceIdentityCertFile is valid in FILE mode. It specifies the file path for service identity certificate.
	ServiceIdentityCertFile string

	// ServiceIdentityPrivateKeyFile is valid in FILE mode. It specifies the file path for service identity private key.
	ServiceIdentityPrivateKeyFile string
}

// NewSecretFileServerConfig creates a Config for propogating key/cert to workload through file.
func NewSecretFileServerConfig(certFile string, keyFile string) Config {
	return Config{
		Mode:                          SecretFile,
		FileUtil:                      util.FileUtilImpl{},
		ServiceIdentityCertFile:       certFile,
		ServiceIdentityPrivateKeyFile: keyFile,
	}
}
