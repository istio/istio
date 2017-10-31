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

package testutil

import (
	"crypto/rsa"
	"crypto/x509"
	"fmt"
	"reflect"
	"strings"
	"time"

	"istio.io/istio/auth/pkg/pki"
)

// VerifyFields contains the certficate fields to verify in the test.
type VerifyFields struct {
	NotBefore   time.Time
	NotAfter    time.Time
	ExtKeyUsage []x509.ExtKeyUsage
	KeyUsage    x509.KeyUsage
	IsCA        bool
	Org         string
}

// VerifyCertificate verifies a given PEM encoded certificate by
// - building one or more chains from the certificate to a root certificate;
// - checking fields are set as expected.
func VerifyCertificate(privPem []byte, certChainPem []byte, rootCertPem []byte,
	host string, expectedFields *VerifyFields) error {

	roots := x509.NewCertPool()
	if rootCertPem != nil {
		if ok := roots.AppendCertsFromPEM(rootCertPem); !ok {
			return fmt.Errorf("failed to parse root certificate")
		}
	}

	intermediates := x509.NewCertPool()
	if ok := intermediates.AppendCertsFromPEM(certChainPem); !ok {
		return fmt.Errorf("failed to parse certificate chain")
	}

	cert, err := pki.ParsePemEncodedCertificate(certChainPem)
	if err != nil {
		return err
	}

	san := host
	// uri scheme is currently not supported in go VerifyOptions. We verify
	// this uri at the end as a special case.
	if strings.HasPrefix(host, "spiffe") {
		san = ""
	}
	opts := x509.VerifyOptions{
		DNSName:       san,
		Intermediates: intermediates,
		Roots:         roots,
	}
	opts.KeyUsages = append(opts.KeyUsages, x509.ExtKeyUsageAny)

	if _, err = cert.Verify(opts); err != nil {
		return fmt.Errorf("failed to verify certificate: " + err.Error())
	}

	priv, err := pki.ParsePemEncodedKey(privPem)
	if err != nil {
		return err
	}

	if !reflect.DeepEqual(priv.(*rsa.PrivateKey).PublicKey, *cert.PublicKey.(*rsa.PublicKey)) {
		return fmt.Errorf("the generated private key and cert doesn't match")
	}

	if strings.HasPrefix(host, "spiffe") {
		matchHost := false
		for _, e := range cert.Extensions {
			if strings.HasSuffix(string(e.Value[:]), host) {
				matchHost = true
				break
			}
		}
		if !matchHost {
			return fmt.Errorf("the certificate doesn't have the expected SAN for: %s", host)
		}
	}

	if na := expectedFields.NotAfter; !na.IsZero() && !na.Equal(cert.NotAfter) {
		return fmt.Errorf("Unexpected value for 'NotAfter' field: want %v but got %v", na, cert.NotAfter)
	}

	if nb := expectedFields.NotBefore; !nb.IsZero() && !nb.Equal(cert.NotBefore) {
		return fmt.Errorf("Unexpected value for 'NotBefore' field: want %v but got %v", nb, cert.NotBefore)
	}

	if eku := expectedFields.ExtKeyUsage; !reflect.DeepEqual(eku, cert.ExtKeyUsage) {
		return fmt.Errorf("Unexpected value for 'ExtKeyUsage' field: want %v but got %v", eku, cert.ExtKeyUsage)
	}

	if ku := expectedFields.KeyUsage; ku != cert.KeyUsage {
		return fmt.Errorf("Unexpected value for 'KeyUsage' field: want %v but got %v", ku, cert.KeyUsage)
	}

	if isCA := expectedFields.IsCA; isCA != cert.IsCA {
		return fmt.Errorf("Unexpected value for 'IsCA' field: want %t but got %t", isCA, cert.IsCA)
	}

	if org := expectedFields.Org; org != "" && !reflect.DeepEqual([]string{org}, cert.Issuer.Organization) {
		return fmt.Errorf("Unexpected value for 'Organization' field: want %v but got %v",
			[]string{org}, cert.Issuer.Organization)
	}

	return nil
}
