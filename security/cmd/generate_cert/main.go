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
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/pki/util"
)

// Layout for parsing time
const timeLayout = "Jan 2 15:04:05 2006"

var (
	certOptions util.CertOptions

	validFrom      string
	signerCertFile string
	signerPrivFile string
	outCert        string
	outPriv        string
)

func init() {
	//CertOptions arguments
	flag.StringVar(&certOptions.Host, "host", "", "Comma-separated hostnames and IPs to generate a certificate for.")
	flag.DurationVar(&certOptions.TTL, "duration", 365*24*time.Hour, "Duration that certificate is valid for.")
	flag.BoolVar(&certOptions.IsCA, "ca", false, "Whether this cert should be a Cerificate Authority.")
	flag.BoolVar(&certOptions.IsSelfSigned, "self-signed", false, "Whether this cerificate is self-signed.")
	flag.BoolVar(&certOptions.IsClient, "client", false, "Whether this certificate is for a client.")
	flag.StringVar(&certOptions.Org, "organization", "Juju org", "Organization for the cert.")
	flag.IntVar(&certOptions.RSAKeySize, "key-size", 2048, "Size of the generated private key")

	flag.StringVar(&validFrom, "start-date", "", "Creation date in format of "+timeLayout)
	flag.StringVar(&signerCertFile, "signer-cert", "", "Signer certificate file (PEM encoded).")
	flag.StringVar(&signerPrivFile, "signer-priv", "", "Signer private key file (PEM encoded).")
	flag.StringVar(&outCert, "out-cert", "cert.pem", "Output certificate file.")
	flag.StringVar(&outPriv, "out-priv", "priv.pem", "Output private key file.")
}
func fatalf(template string, args ...interface{}) {
	log.Errorf(template, args)
	os.Exit(-1)
}

func checkCmdLine() {
	flag.Parse()

	hasCert, hasPriv := len(signerCertFile) != 0, len(signerPrivFile) != 0
	if certOptions.IsSelfSigned {
		if hasCert || hasPriv {
			fatalf("--self-signed is incompatible with --signer-cert or --signer-priv.")
		}
	} else {
		if !hasCert && !hasPriv {
			fatalf("Need --self-signed or --signer-cert and --signer-priv.")
		} else if !(hasCert && hasPriv) {
			fatalf("Missing --signer-cert or --signer-priv.")
		}
	}
}

func saveCreds(certPem []byte, privPem []byte) {
	err := ioutil.WriteFile(outCert, certPem, 0644)
	if err != nil {
		fatalf("Could not write output certificate: %s.", err)
	}

	err = ioutil.WriteFile(outPriv, privPem, 0600)
	if err != nil {
		fatalf("Could not write output private key: %s.", err)
	}
}

func main() {
	checkCmdLine()

	var err error
	if !certOptions.IsSelfSigned {
		certOptions.SignerCert, certOptions.SignerPriv, err = util.LoadSignerCredsFromFiles(signerCertFile, signerPrivFile)
		if err != nil {
			log.Errora(err)
			os.Exit(-1)
		}
	}

	certOptions.NotBefore = getNotBefore()
	certPem, privPem, err := util.GenCertKeyFromOptions(certOptions)

	if err != nil {
		log.Errora(err)
		os.Exit(-1)
	}

	saveCreds(certPem, privPem)
	fmt.Printf("Certificate and private files successfully saved in %s and %s\n", outCert, outPriv)
}

func getNotBefore() time.Time {
	if validFrom == "" {
		return time.Now()
	}

	t, err := time.Parse(timeLayout, validFrom)
	if err != nil {
		fatalf("Failed to parse the '-start-from' option as a time (error: %s)", err)
	}

	return t
}
