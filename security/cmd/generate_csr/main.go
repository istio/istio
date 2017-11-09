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

// Provide a tool to generate X.509 CSR with different options.

package main

import (
	"flag"
	"fmt"
	"io/ioutil"

	"github.com/golang/glog"

	"istio.io/istio/security/pkg/pki/ca"
)

var (
	host    = flag.String("host", "", "Comma-separated hostnames and IPs to generate a certificate for.")
	org     = flag.String("organization", "Juju org", "Organization for the cert.")
	outCsr  = flag.String("out-csr", "csr.pem", "Output csr file.")
	outPriv = flag.String("out-priv", "priv.pem", "Output private key file.")
	keySize = flag.Int("key-size", 2048, "Size of the generated private key")
)

func saveCreds(csrPem []byte, privPem []byte) {
	err := ioutil.WriteFile(*outCsr, csrPem, 0644)
	if err != nil {
		glog.Fatalf("Could not write output certificate request: %s.", err)
	}

	err = ioutil.WriteFile(*outPriv, privPem, 0600)
	if err != nil {
		glog.Fatalf("Could not write output private key: %s.", err)
	}
}

func main() {
	flag.Parse()

	csrPem, privPem, err := ca.GenCSR(ca.CertOptions{
		Host:       *host,
		Org:        *org,
		RSAKeySize: *keySize,
	})

	if err != nil {
		glog.Fatalf("Failed to generate CSR: %s.", err)
	}

	saveCreds(csrPem, privPem)
	fmt.Printf("Certificate and private files successfully saved in %s and %s\n", *outCsr, *outPriv)
}
