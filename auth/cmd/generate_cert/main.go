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
	"crypto/x509"
	"crypto/rsa"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"time"

	"istio.io/auth/cert_manager"
)

var (
	host             = flag.String("host", "", "Comma-separated hostnames and IPs to generate a certificate for.")
	validFrom        = flag.String("start-date", "", "Creation date formatted as Jan 1 15:04:05 2011.")
	validFor         = flag.Duration("duration", 365*24*time.Hour, "Duration that certificate is valid for.")
	isCA             = flag.Bool("ca", false, "Whether this cert should be a Cerificate Authority.")
	isSelfSigned     = flag.Bool("self-signed", false, "Whether this cerificate is self-signed.")
	signerPrivFile   = flag.String("signer-priv", "", "Signer private key file (PEM encoded).")
	signerCertFile   = flag.String("signer-cert", "", "Signer certificate file (PEM encoded).")
	isClient         = flag.Bool("client", false, "Whether this certificate is for a client.")
	org              = flag.String("organization", "Juju org", "Organization for the cert.")
	outCert          = flag.String("out-cert", "cert.pem", "Output certificate file.")
	outPriv          = flag.String("out-priv", "priv.pem", "Output private key file.")
)

func checkCmdLine() {
	flag.Parse()
	if len(*host) == 0 {
		log.Fatalf("Missing required --host parameter.")
	}
	hasPriv, hasCert := len(*signerPrivFile) != 0, len(*signerCertFile) != 0
	if *isSelfSigned {
		if hasPriv || hasCert {
			log.Fatalf("--self-signed is incompatible with --signer-priv or --signer-cert.")
		}
	} else {
		if !hasPriv && !hasCert {
			log.Fatalf("Need --self-signed or --signer-priv and --signer-cert.")
		} else if !(hasPriv && hasCert) {
			log.Fatalf("Missing --signer-cert or --signer-priv.")
		}
	}
}

func saveCreds(privPem []byte, certPem []byte) {
	err := ioutil.WriteFile(*outCert, certPem, 0644)
	if err != nil {
		log.Fatalf("Could not write output certificate: %s.", err)
	}

	err = ioutil.WriteFile(*outPriv, privPem, 0600)
	if err != nil {
		log.Fatalf("Could not write output private key: %s.", err)
	}
}

func main() {
	checkCmdLine()

	var signerCert *x509.Certificate = nil
	var signerPriv *rsa.PrivateKey = nil
	if !*isSelfSigned {
	  signerCert, signerPriv = cert_manager.LoadSigningCreds(*signerCertFile, *signerPrivFile)
	}

	privPem, certPem := cert_manager.GenCert(cert_manager.CertOptions{
		Host: *host,
		ValidFrom: *validFrom,
		ValidFor: *validFor,
		SignerCert: signerCert,
		SignerPriv: signerPriv,
		Org: *org,
		IsCA: *isCA,
		IsSelfSigned: *isSelfSigned,
		IsClient: *isClient})

	saveCreds(privPem, certPem)
	fmt.Printf("Certificate and private files successfully saved in %s and %s\n", *outCert, *outPriv)
}
