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

const (
	keyFilePermission  = 0600
	certFilePermission = 0644
)

// SecretFileServer is an implementation of SecretServer that writes the key/cert into file system.
type SecretFileServer struct {
	cfg Config
}

// SetServiceIdentityPrivateKey sets the service identity private key into the file system.
func (sf *SecretFileServer) SetServiceIdentityPrivateKey(content []byte) error {
	return sf.cfg.FileUtil.Write(sf.cfg.ServiceIdentityPrivateKeyFile, content, keyFilePermission)
}

// SetServiceIdentityCert sets the service identity certificate into the file system.
func (sf *SecretFileServer) SetServiceIdentityCert(content []byte) error {
	return sf.cfg.FileUtil.Write(sf.cfg.ServiceIdentityCertFile, content, certFilePermission)
}
