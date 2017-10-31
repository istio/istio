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

// Provide a tool to generate X.509 certificate with different options.

package main

import (
	"crypto"
	"crypto/x509"
	"flag"
	"fmt"
	"io/ioutil"
	"time"

	"istio.io/istio/auth/pkg/pki/ca"

	"github.com/golang/glog"
)

// Layout for parsing time
const timeLayout = "Jan 2 15:04:05 2006"

var (
	host           = flag.String("host", "", "Comma-separated hostnames and IPs to generate a certificate for.")
	validFrom      = flag.String("start-date", "", "Creation date in format of "+timeLayout)
	validFor       = flag.Duration("duration", 365*24*time.Hour, "Duration that certificate is valid for.")
	isCA           = flag.Bool("ca", false, "Whether this cert should be a Cerificate Authority.")
	isSelfSigned   = flag.Bool("self-signed", false, "Whether this cerificate is self-signed.")
	signerCertFile = flag.String("signer-cert", "", "Signer certificate file (PEM encoded).")
	signerPrivFile = flag.String("signer-priv", "", "Signer private key file (PEM encoded).")
	isClient       = flag.Bool("client", false, "Whether this certificate is for a client.")
	org            = flag.String("organization", "Juju org", "Organization for the cert.")
	outCert        = flag.String("out-cert", "cert.pem", "Output certificate file.")
	outPriv        = flag.String("out-priv", "priv.pem", "Output private key file.")
	keySize        = flag.Int("key-size", 1024, "Size of the generated private key")
)

func checkCmdLine() {
	flag.Parse()

	hasCert, hasPriv := len(*signerCertFile) != 0, len(*signerPrivFile) != 0
	if *isSelfSigned {
		if hasCert || hasPriv {
			glog.Fatalf("--self-signed is incompatible with --signer-cert or --signer-priv.")
		}
	} else {
		if !hasCert && !hasPriv {
			glog.Fatalf("Need --self-signed or --signer-cert and --signer-priv.")
		} else if !(hasCert && hasPriv) {
			glog.Fatalf("Missing --signer-cert or --signer-priv.")
		}
	}
}

func saveCreds(certPem []byte, privPem []byte) {
	err := ioutil.WriteFile(*outCert, certPem, 0644)
	if err != nil {
		glog.Fatalf("Could not write output certificate: %s.", err)
	}

	err = ioutil.WriteFile(*outPriv, privPem, 0600)
	if err != nil {
		glog.Fatalf("Could not write output private key: %s.", err)
	}
}

func main() {
	checkCmdLine()

	var signerCert *x509.Certificate
	var signerPriv crypto.PrivateKey
	if !*isSelfSigned {
		signerCert, signerPriv = ca.LoadSignerCredsFromFiles(*signerCertFile, *signerPrivFile)
	}

	nb := getNotBefore()
	certPem, privPem := ca.GenCert(ca.CertOptions{
		Host:         *host,
		NotBefore:    getNotBefore(),
		NotAfter:     nb.Add(*validFor),
		SignerCert:   signerCert,
		SignerPriv:   signerPriv,
		Org:          *org,
		IsCA:         *isCA,
		IsSelfSigned: *isSelfSigned,
		IsClient:     *isClient,
		RSAKeySize:   *keySize,
	})

	saveCreds(certPem, privPem)
	fmt.Printf("Certificate and private files successfully saved in %s and %s\n", *outCert, *outPriv)
}

func getNotBefore() time.Time {
	if *validFrom == "" {
		return time.Now()
	}

	t, err := time.Parse(timeLayout, *validFrom)
	if err != nil {
		glog.Fatalf("Failed to parse the '-start-from' option as a time (error: %s)", err)
	}

	return t
}
