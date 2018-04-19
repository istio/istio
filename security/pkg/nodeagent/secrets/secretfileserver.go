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
	"io/ioutil"
	"os"
	"path"

	"istio.io/istio/security/pkg/pki/util"
)

const (
	// KeyFilePermission is the permission bits for private key file.
	KeyFilePermission = 0600

	// CertFilePermission is the permission bits for certificate file.
	CertFilePermission = 0644
)

// SecretFileServer is an implementation of SecretServer that writes the key/cert into file system.
type SecretFileServer struct {
	rootDir string
}

// Put writes the specified key and cert to the files.
func (sf *SecretFileServer) Put(serviceAccount string, keycert util.KeyCertBundle) error {
	_, priv, cert, root := keycert.GetAllPem()
	dir := path.Join(sf.rootDir, serviceAccount)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.Mkdir(dir, 0700); err != nil {
			return fmt.Errorf("failed to create directory for %v, err %v", serviceAccount, err)
		}
	}
	kpath := path.Join(dir, "key.pem")
	if err := ioutil.WriteFile(kpath, priv, KeyFilePermission); err != nil {
		return err
	}
	cpath := path.Join(dir, "cert-chain.pem")
	if err := ioutil.WriteFile(cpath, cert, CertFilePermission); err != nil {
		return err
	}
	rpath := path.Join(dir, "root-cert.pem")
	return ioutil.WriteFile(rpath, root, CertFilePermission)
}
