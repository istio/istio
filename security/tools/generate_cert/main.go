// Copyright Istio Authors
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
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os/exec"
	"time"

	k8s "k8s.io/api/core/v1"

	"istio.io/istio/security/pkg/pki/util"
)

const (
	// Layout for parsing time
	timeLayout = "Jan 2 15:04:05 2006"

	// modes supported by this tool.
	selfSignedMode = "self-signed"
	signerMode     = "signer"
	citadelMode    = "citadel"
)

var (
	host           = flag.String("host", "", "Comma-separated hostnames and IPs to generate a certificate for.")
	validFrom      = flag.String("start-date", "", "Creation date in format of "+timeLayout)
	validFor       = flag.Duration("duration", 365*24*time.Hour, "Duration that certificate is valid for.")
	isCA           = flag.Bool("ca", false, "Whether this cert should be a Certificate Authority.")
	signerCertFile = flag.String("signer-cert", "", "Signer certificate file (PEM encoded).")
	signerPrivFile = flag.String("signer-priv", "", "Signer private key file (PEM encoded).")
	isClient       = flag.Bool("client", false, "Whether this certificate is for a client.")
	org            = flag.String("organization", "Juju org", "Organization for the cert.")
	outCert        = flag.String("out-cert", "cert.pem", "Output certificate file.")
	outPriv        = flag.String("out-priv", "priv.pem", "Output private key file.")
	keySize        = flag.Int("key-size", 2048, "Size of the generated private key")
	mode           = flag.String("mode", selfSignedMode, "Supported mode: self-signed, signer, citadel")
	//Enable this flag if istio mTLS is enabled and the service is running as server side
	isServer = flag.Bool("server", false, "Whether this certificate is for a server.")
	ec       = flag.String("ec-sig-alg", "", "Generate an elliptical curve private key with the specified algorithm")
)

func checkCmdLine() {
	flag.Parse()

	hasCert, hasPriv := len(*signerCertFile) != 0, len(*signerPrivFile) != 0
	switch *mode {
	case selfSignedMode:
		if hasCert || hasPriv {
			log.Fatalf("--mode=%v is incompatible with --signer-cert or --signer-priv.", selfSignedMode)
		}
	case signerMode:
		if !hasCert || !hasPriv {
			log.Fatalf("Need --signer-cert and --signer-priv for --mode=%v.", signerMode)
		}
	case citadelMode:
		if hasCert || hasPriv {
			log.Fatalf("--mode=%v is incompatible with --signer-cert or --signer-priv.", citadelMode)
		}
	default:
		log.Fatalf("Unsupported mode %v", *mode)
	}
}

func saveCreds(certPem []byte, privPem []byte) {
	err := ioutil.WriteFile(*outCert, certPem, 0644)
	if err != nil {
		log.Fatalf("Could not write output certificate: %s.", err)
	}

	err = ioutil.WriteFile(*outPriv, privPem, 0600)
	if err != nil {
		log.Fatalf("Could not write output private key: %s.", err)
	}
}

func signCertFromCitadel() (*x509.Certificate, crypto.PrivateKey) {
	args := []string{"get", "secret", "-n", "istio-system", "istio-ca-secret", "-o", "json"}
	cmd := exec.Command("kubectl", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Fatalf("Command failed error: %v\n, output\n%v\n", err, string(out))
	}

	var secret k8s.Secret
	err = json.Unmarshal(out, &secret)
	if err != nil {
		log.Fatalf("Unmarshal secret error: %v", err)
	}
	key, err := util.ParsePemEncodedKey(secret.Data["ca-key.pem"])
	if err != nil {
		log.Fatalf("Unrecognized key format from citadel %v", err)
	}
	cert, err := util.ParsePemEncodedCertificate(secret.Data["ca-cert.pem"])
	if err != nil {
		log.Fatalf("Unrecognized cert format from citadel %v", err)
	}
	return cert, key
}

func main() {
	checkCmdLine()

	var signerCert *x509.Certificate
	var signerPriv crypto.PrivateKey
	var err error
	switch *mode {
	case selfSignedMode:
	case signerMode:
		signerCert, signerPriv, err = util.LoadSignerCredsFromFiles(*signerCertFile, *signerPrivFile)
		if err != nil {
			log.Fatalf("Failed to load signer key cert from file: %v\n", err)
		}
	case citadelMode:
		signerCert, signerPriv = signCertFromCitadel()
	default:
		log.Fatalf("Unsupported mode %v", *mode)
	}

	opts := util.CertOptions{
		Host:         *host,
		NotBefore:    getNotBefore(),
		TTL:          *validFor,
		SignerCert:   signerCert,
		SignerPriv:   signerPriv,
		Org:          *org,
		IsCA:         *isCA,
		IsSelfSigned: *mode == selfSignedMode,
		IsClient:     *isClient,
		RSAKeySize:   *keySize,
		IsServer:     *isServer,
		ECSigAlg:     util.SupportedECSignatureAlgorithms(*ec),
	}
	certPem, privPem, err := util.GenCertKeyFromOptions(opts)

	if err != nil {
		log.Fatalf("Failed to generate certificate: %v\n", err)
	}

	saveCreds(certPem, privPem)
	fmt.Printf("Certificate and private files successfully saved in %s and %s\n", *outCert, *outPriv)
}

func getNotBefore() time.Time {
	if *validFrom == "" {
		return time.Now()
	}

	t, err := time.Parse(timeLayout, *validFrom)
	if err != nil {
		log.Fatalf("Failed to parse the '-start-from' option as a time (error: %s)\n", err)
	}

	return t
}
