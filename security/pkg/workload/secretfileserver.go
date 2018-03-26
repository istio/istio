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
	"io/ioutil"

	"path"

	"istio.io/istio/security/pkg/pki/util"
)

const (
	// KeyFilePermission is the permission bits for private key file.
	KeyFilePermission  = 0600

	// CertFilePermission is the permission bits for certificate file.
	CertFilePermission = 0644
)

// SecretFileServer is an implementation of SecretServer that writes the key/cert into file system.
type SecretFileServer struct {
	rootDir string
}

// Save writes the specified key and cert to the files.
// TODO(incfly): need to add more test for isolation.
func (sf *SecretFileServer) Save(keycert util.KeyCertBundle) error {
	cert, priv, _, _ := keycert.GetAllPem()
	id := util.RetrieveID(keycert)
	cpath := path.Join(sf.rootDir, id, "cert-chain.pem")
	kpath := path.Join(sf.rootDir, id, "key.pem")
	if err := ioutil.WriteFile(cpath, priv, KeyFilePermission); err != nil {
		return err
	}
	return ioutil.WriteFile(kpath, cert, CertFilePermission)
}
