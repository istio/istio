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

package util

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

// VerifyFields contains the certificate fields to verify in the test.
type VerifyFields struct {
	NotBefore   time.Time
	TTL         time.Duration // NotAfter - NotBefore
	ExtKeyUsage []x509.ExtKeyUsage
	KeyUsage    x509.KeyUsage
	IsCA        bool
	Org         string
	CommonName  string
	Host        string
}

// VerifyCertificate verifies a given PEM encoded certificate by
// - building one or more chains from the certificate to a root certificate;
// - checking fields are set as expected.
func VerifyCertificate(privPem []byte, certChainPem []byte, rootCertPem []byte, expectedFields *VerifyFields) error {

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

	cert, err := ParsePemEncodedCertificate(certChainPem)
	if err != nil {
		return err
	}

	host := expectedFields.Host
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

	priv, err := ParsePemEncodedKey(privPem)
	if err != nil {
		return err
	}

	privRSAKey, privRSAOk := priv.(*rsa.PrivateKey)
	pubRSAKey, pubRSAOk := cert.PublicKey.(*rsa.PublicKey)

	privECKey, privECOk := priv.(*ecdsa.PrivateKey)
	pubECKey, pubECOk := cert.PublicKey.(*ecdsa.PublicKey)

	rsaMatch := privRSAOk && pubRSAOk
	ecMatch := privECOk && pubECOk

	if rsaMatch {
		if !reflect.DeepEqual(privRSAKey.PublicKey, *pubRSAKey) {
			return fmt.Errorf("the generated private RSA key and cert doesn't match")
		}
	} else if ecMatch {
		if !reflect.DeepEqual(privECKey.PublicKey, *pubECKey) {
			return fmt.Errorf("the generated private EC key and cert doesn't match")
		}
	} else {
		return fmt.Errorf("algorithms for private key and cert do not match")
	}

	if strings.HasPrefix(host, "spiffe") {
		matchHost := false
		ids, err := ExtractIDs(cert.Extensions)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if strings.HasSuffix(id, host) {
				matchHost = true
				break
			}
		}
		if !matchHost {
			return fmt.Errorf("the certificate doesn't have the expected SAN for: %s", host)
		}
	}

	if nb := expectedFields.NotBefore; !nb.IsZero() && !nb.Equal(cert.NotBefore) {
		return fmt.Errorf("unexpected value for 'NotBefore' field: want %v but got %v", nb, cert.NotBefore)
	}

	if ttl := expectedFields.TTL; ttl != 0 && ttl != (cert.NotAfter.Sub(cert.NotBefore)) {
		return fmt.Errorf("unexpected value for 'NotAfter' - 'NotBefore': want %v but got %v", ttl, cert.NotAfter.Sub(cert.NotBefore))
	}

	if eku := sortExtKeyUsage(expectedFields.ExtKeyUsage); !reflect.DeepEqual(eku, sortExtKeyUsage(cert.ExtKeyUsage)) {
		return fmt.Errorf("unexpected value for 'ExtKeyUsage' field: want %v but got %v", eku, cert.ExtKeyUsage)
	}

	if ku := expectedFields.KeyUsage; ku != cert.KeyUsage {
		return fmt.Errorf("unexpected value for 'KeyUsage' field: want %v but got %v", ku, cert.KeyUsage)
	}

	if isCA := expectedFields.IsCA; isCA != cert.IsCA {
		return fmt.Errorf("unexpected value for 'IsCA' field: want %t but got %t", isCA, cert.IsCA)
	}

	if org := expectedFields.Org; org != "" && !reflect.DeepEqual([]string{org}, cert.Issuer.Organization) {
		return fmt.Errorf("unexpected value for 'Organization' field: want %v but got %v",
			[]string{org}, cert.Issuer.Organization)
	}

	if cn := expectedFields.CommonName; cn != cert.Subject.CommonName {
		return fmt.Errorf("unexpected value for 'CommonName' field: want %v but got %v",
			cn, cert.Subject.CommonName)
	}

	return nil
}

func sortExtKeyUsage(extKeyUsage []x509.ExtKeyUsage) []int {
	data := make([]int, len(extKeyUsage))
	for i := range extKeyUsage {
		data[i] = int(extKeyUsage[i])
	}
	sort.Ints(data)
	return data
}
