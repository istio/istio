//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package ca

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"istio.io/istio/pkg/test/cert"
)

var (
	rootCAConf = `
[ req ]
encrypt_key = no
prompt = no
utf8 = yes
default_md = sha256
default_bits = 4096
req_extensions = req_ext
x509_extensions = req_ext
distinguished_name = req_dn
[ req_ext ]
subjectKeyIdentifier = hash
basicConstraints = critical, CA:true
keyUsage = critical, digitalSignature, nonRepudiation, keyEncipherment, keyCertSign
[ req_dn ]
O = Istio
CN = Root CA`
)

// Root contains the cryptographic files for a self-signed root CA.
type Root struct {
	// KeyFile is the path to the file containing the private key for the CA.
	KeyFile string

	// ConfFile is the path to the file containing the extensions configuration file.
	ConfFile string

	// CSRFile used to generate the cert.
	CSRFile string

	// CertFile the cert for the root CA.
	CertFile string
}

// NewRoot generates the files for a new self-signed Root CA files under the given directory.
func NewRoot(workDir string) (Root, error) {
	root := Root{
		KeyFile:  filepath.Join(workDir, "root-key.pem"),
		ConfFile: filepath.Join(workDir, "root-ca.conf"),
		CSRFile:  filepath.Join(workDir, "root-ca.csr"),
		CertFile: filepath.Join(workDir, "root-cert.pem"),
	}

	// Write out the conf file.
	if err := ioutil.WriteFile(root.ConfFile, []byte(rootCAConf), os.ModePerm); err != nil {
		return Root{}, err
	}

	// Create the root key.
	if err := cert.GenerateKey(root.KeyFile); err != nil {
		return Root{}, err
	}

	// Create the root CSR
	if err := cert.GenerateCSR(root.ConfFile, root.KeyFile, root.CSRFile); err != nil {
		return Root{}, err
	}

	// Create the root cert
	if err := cert.GenerateCert(root.ConfFile, root.CSRFile, root.KeyFile, root.CertFile); err != nil {
		return Root{}, err
	}
	return root, nil
}
