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

package istio

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	kubeApiCore "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/tmpl"
)

var (
	rootCAConf = fmt.Sprintf(`
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
CN = Root CA`)

	clusterCAConfTemplate = `
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
basicConstraints = critical, CA:true, pathlen:0
keyUsage = critical, digitalSignature, nonRepudiation, keyEncipherment, keyCertSign
subjectAltName=@san
[ san ]
URI.1 = spiffe://cluster.local/ns/{{ .SystemNamespace }}/sa/citadel
DNS.1 = istiod.{{ .SystemNamespace }}
DNS.2 = istiod.{{ .SystemNamespace }}.svc
DNS.3 = istio-pilot.{{ .SystemNamespace }}
DNS.4 = istio-pilot.{{ .SystemNamespace }}.svc
[ req_dn ]
O = Istio
CN = Intermediate CA
`
)

func newSelfSignedRootCA(workDir string) (rootCA, error) {
	dir := filepath.Join(workDir, "cacerts")
	if err := os.Mkdir(dir, 0700); err != nil {
		return rootCA{}, err
	}

	root := rootCA{
		dir:      dir,
		keyFile:  filepath.Join(dir, "root-key.pem"),
		confFile: filepath.Join(dir, "root-ca.conf"),
		csrFile:  filepath.Join(dir, "root-ca.csr"),
		certFile: filepath.Join(dir, "root-cert.pem"),
	}

	// Write out the root CA conf file.
	if err := ioutil.WriteFile(root.confFile, []byte(rootCAConf), os.ModePerm); err != nil {
		return rootCA{}, err
	}

	// Create the root key.
	if err := openssl("genrsa",
		"-out", root.keyFile, "4096"); err != nil {
		return rootCA{}, err
	}

	// Create the root CSR
	if err := openssl("req", "-new",
		"-key", root.keyFile,
		"-config", root.confFile,
		"-out", root.csrFile); err != nil {
		return rootCA{}, err
	}

	// Create the root cert
	if err := openssl("x509", "-req",
		"-days", "100000",
		"-signkey", root.keyFile,
		"-extensions", "req_ext",
		"-extfile", root.confFile,
		"-in", root.csrFile,
		"-out", root.certFile); err != nil {
		return rootCA{}, err
	}
	return root, nil
}

// rootCA used to generate intermediate cluster CAs.
type rootCA struct {
	keyFile  string
	confFile string
	csrFile  string
	certFile string
	dir      string
}

// newClusterCA creates a new intermediate CA for the given cluster.
func (root rootCA) newClusterCA(cluster kube.Cluster, cfg Config) (clusterCA, error) {
	// Make a subdirectory for the intermediate certs for this cluster.
	dir := filepath.Join(root.dir, cluster.Name())
	if err := os.Mkdir(dir, 0700); err != nil {
		return clusterCA{}, err
	}

	ca := clusterCA{
		keyFile:  filepath.Join(dir, "ca-key.pem"),
		confFile: filepath.Join(dir, "cluster-ca.conf"),
		csrFile:  filepath.Join(dir, "cluster-ca.csr"),
		certFile: filepath.Join(dir, "root-cert.pem"),
		root:     root,
	}

	// Write out the CA config file.
	config, err := tmpl.Evaluate(clusterCAConfTemplate, cfg)
	if err != nil {
		return clusterCA{}, err
	}
	if err := ioutil.WriteFile(ca.confFile, []byte(config), os.ModePerm); err != nil {
		return clusterCA{}, err
	}

	// Create the key for the intermediate CA.
	if err := openssl("genrsa",
		"-out", ca.keyFile, "4096"); err != nil {
		return clusterCA{}, err
	}

	// Create the CSR for the intermediate CA.
	if err := openssl("req", "-new",
		"-config", ca.confFile,
		"-key", ca.keyFile,
		"-out", ca.csrFile); err != nil {
		return clusterCA{}, err
	}

	// Create the intermediate cert, signed by the root.
	if err := openssl("x509", "-req",
		"-days", "100000",
		"-CA", root.certFile,
		"-CAkey", root.keyFile,
		"-CAcreateserial",
		"-extensions", "req_ext",
		"-extfile", ca.confFile,
		"-in", ca.csrFile,
		"-out", ca.certFile); err != nil {
		return clusterCA{}, err
	}

	return ca, nil
}

// clusterCA is an intermediate CA for a single cluster.
type clusterCA struct {
	keyFile  string
	confFile string
	csrFile  string
	certFile string
	root     rootCA
}

func (ca clusterCA) NewSecret() (*kubeApiCore.Secret, error) {
	caCert, err := file.AsString(ca.certFile)
	if err != nil {
		return nil, err
	}
	caKey, err := file.AsString(ca.keyFile)
	if err != nil {
		return nil, err
	}
	rootCert, err := file.AsString(ca.root.certFile)
	if err != nil {
		return nil, err
	}

	// Create the cert chain by concatenating the intermediate and root certs.
	certChain := caCert + rootCert

	return &kubeApiCore.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cacerts",
		},
		Data: map[string][]byte{
			"ca-cert.pem":    []byte(caCert),
			"ca-key.pem":     []byte(caKey),
			"cert-chain.pem": []byte(certChain),
			"root-cert.pem":  []byte(rootCert),
		},
	}, nil
}

func openssl(args ...string) error {
	cmd := exec.Command("openssl", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("command %s failed: %q %v", cmd.String(), string(out), err)
	}
	return nil
}
