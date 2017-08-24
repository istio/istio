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

package ca

import (
	"bytes"
	"crypto/x509"
	"encoding/asn1"
	"fmt"
	"reflect"
	"testing"
	"time"

	"istio.io/auth/pkg/pki"
	"istio.io/auth/pkg/pki/testutil"
)

func TestSelfSignedIstioCA(t *testing.T) {
	certTTL := 30 * time.Minute
	caCertTTL := time.Hour
	org := "test.ca.org"
	ca, err := NewSelfSignedIstioCA(caCertTTL, certTTL, org)
	if err != nil {
		t.Errorf("Failed to create a self-signed CA: %v", err)
	}

	name := "foo"
	namespace := "bar"
	id := fmt.Sprintf("spiffe://cluster.local/ns/%s/sa/%s", namespace, name)
	options := CertOptions{
		Host:       id,
		RSAKeySize: 1024,
	}
	csr, _, err := GenCSR(options)
	if err != nil {
		t.Error(err)
	}
	cb, err := ca.Sign(csr)
	if err != nil {
		t.Error(err)
	}

	rcb := ca.GetRootCertificate()

	certPool := x509.NewCertPool()
	certPool.AppendCertsFromPEM(cb)

	rootPool := x509.NewCertPool()
	rootPool.AppendCertsFromPEM(rcb)

	cert, err := pki.ParsePemEncodedCertificate(cb)
	if err != nil {
		t.Error(err)
	}
	if ttl := cert.NotAfter.Sub(cert.NotBefore); ttl != certTTL {
		t.Errorf("Unexpected certificate TTL (expecting %v, actual %v)", certTTL, ttl)
	}

	rootCert, err := pki.ParsePemEncodedCertificate(rcb)
	if err != nil {
		t.Error(err)
	}
	if ttl := rootCert.NotAfter.Sub(rootCert.NotBefore); ttl != caCertTTL {
		t.Errorf("Unexpected CA certificate TTL (expecting %v, actual %v)", caCertTTL, ttl)
	}
	if certOrg := rootCert.Issuer.Organization[0]; certOrg != org {
		t.Errorf("Unexpected CA certificate organization (expecting %v, actual %v)", org, certOrg)
	}

	chain, err := cert.Verify(x509.VerifyOptions{
		Intermediates: certPool,
		Roots:         rootPool,
	})
	if len(chain) == 0 || err != nil {
		t.Error("Failed to verify generated cert")
	}

	san := pki.ExtractSANExtension(cert.Extensions)
	if san == nil {
		t.Errorf("Generated certificate does not contain a SAN field")
	}

	rv := asn1.RawValue{Tag: 6, Class: asn1.ClassContextSpecific, Bytes: []byte(id)}
	bs, err := asn1.Marshal([]asn1.RawValue{rv})
	if err != nil {
		t.Error(err)
	}

	if !bytes.Equal(bs, san.Value) {
		t.Errorf("SAN field does not match: %s is expected but actual is %s", bs, san.Value)
	}
}

// Pass in unmatched chain and cert to make sure the `verify` method yeilds an error.
func TestInvalidIstioCAOptions(t *testing.T) {
	rootCert := `
-----BEGIN CERTIFICATE-----
MIIDXTCCAkWgAwIBAgIJAPa8VTmVboq0MA0GCSqGSIb3DQEBCwUAMEUxCzAJBgNV
BAYTAkFVMRMwEQYDVQQIDApTb21lLVN0YXRlMSEwHwYDVQQKDBhJbnRlcm5ldCBX
aWRnaXRzIFB0eSBMdGQwHhcNMTcwMzE4MDAxMDI5WhcNMjcwMzE2MDAxMDI5WjBF
MQswCQYDVQQGEwJBVTETMBEGA1UECAwKU29tZS1TdGF0ZTEhMB8GA1UECgwYSW50
ZXJuZXQgV2lkZ2l0cyBQdHkgTHRkMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIB
CgKCAQEAsBOcKtPZMB32Un0r0Ew8X4n12xgoW+2Z5f7p8reY80U6JrMPIK6yuQWk
juGQsIFhWma0ELRB7xCQJZghEc6MyDR0PfESsljDZebYL7ZHlE9xWcZ2+qw3YFca
wtRLa2Mud0Rx7pXMj07JGiyJ5bM5t1KJP4Wz04ZXHUDOa0NYsoFl8hJwXV/AIY0D
2+dcwa/XN0pWtgztoHL52XzliKpVPqHkgZNN7UAO6ym7pr1JRATW572YsnkLFwgg
4GJ6Nyoh3ZUghS918aZVXHQNfyvF5yAMOn47b5Zbk82ZT6ZDB2KLFJE4/F0OeryZ
ncbW6HA2j0GBQPICl9+NW+Ud4KCzuwIDAQABo1AwTjAdBgNVHQ4EFgQU2Cr6Z6wH
hBBYnid52DEESkDX4J0wHwYDVR0jBBgwFoAU2Cr6Z6wHhBBYnid52DEESkDX4J0w
DAYDVR0TBAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAQEAB8nNUdDZ0pNX8ZGzQbgj
2wCjaX0Za0BNPvVoqpM3oR5BCXodS4HUgx2atpTjsSQlMJzR565OmoykboF5g+K3
hRW6cy4n6LdhY+WvyiyOlbLl+Qj8ceCaBbNrLrg1KbsTI3F8fL1gUzOOr+NNkOJz
MDYxmuy/5kMVUp2uIx7aTigCouKgMyciA0a/FJcy1aLnW06yUj4NK0yBHXwpRMjF
xcOPOXOTlDZkt88KRTveX9zUiCI9o6/lpZEjdHqT8uhXy2v+TY/akM/cuge/PMBz
pspEEzvnu1mW6XEEPgRc8iFZCdtGli6Yfaixxb9oFb/T/vQ4HXh/cb5SddTBPCDS
5Q==
-----END CERTIFICATE-----
	`

	// This signing cert is not signed by the root cert.
	signingCert := `
-----BEGIN CERTIFICATE-----
MIIC5TCCAc2gAwIBAgIQbnMGpidD8PvetlXnYSkUHjANBgkqhkiG9w0BAQsFADAT
MREwDwYDVQQKEwhKdWp1IG9yZzAeFw0xNzAzMTgwMDE5MDZaFw0yNzAzMDYwMDE5
MDZaMBMxETAPBgNVBAoTCEp1anUgb3JnMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A
MIIBCgKCAQEAoEf2+WIjOLOpVBdV6HpgdEgNklJWGNW5kpinW75F2U14/hznSqY+
JbtEPz7MXeWIagpC3gzSNM7Khtdm/jQjdnZuRhRzbBXILCrdRykewUhXsKdtpNpw
bUkCgy7V861zOtwFo3Wm7J7UZIrNqYK8fJrE2YZve9rMyKj1zOVPv6Lm8ioomv2r
DANX0F72+qpEAqxrD5YCexdhv+/WeO3YoEECgqRhCLbG71OzREfN2lrgl7vGpqTA
bUDJK2RxL4yeARU9WcHT2mXplK5w0w63IdgM8kQdodEPHTlP//lafUDq87PjrcTY
eUehLBvtclbEo9bmmnN4JOGNMywVXCw2lQIDAQABozUwMzAOBgNVHQ8BAf8EBAMC
BaAwEwYDVR0lBAwwCgYIKwYBBQUHAwEwDAYDVR0TAQH/BAIwADANBgkqhkiG9w0B
AQsFAAOCAQEABn9FAdcE+N7upOIU2yWalEe0YQgyTELF9MTstJAeJP/xSnCqF6TG
/TfR0IuY/RJyXDLq2rHhrUEsRCCamlQyNkE8RSiHQD/kBf/xxSKobXQyMedXBKSC
MHF2h+S/2HmZaOtgG4RnXplCpHegFOhcLORBLbyQJ72DPLvQcCo2A9uyboqKbZhs
0Gh5kSgZrvphvxIerbV5T/VWLO0llhFmU55BIalVpHD7YfMCOkjVL+Y/0fYKL5ij
68/BQAVGtO+1W1AW52eSMoH1gbvYemf+RsxdE/yKCmcTcZer8HswkQzPH03XcMwu
V611eTJ/uJO6FTt9/5IN8G1qBj2bdNj/uA==
-----END CERTIFICATE-----
	`

	signingKey := `
-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEAoEf2+WIjOLOpVBdV6HpgdEgNklJWGNW5kpinW75F2U14/hzn
SqY+JbtEPz7MXeWIagpC3gzSNM7Khtdm/jQjdnZuRhRzbBXILCrdRykewUhXsKdt
pNpwbUkCgy7V861zOtwFo3Wm7J7UZIrNqYK8fJrE2YZve9rMyKj1zOVPv6Lm8ioo
mv2rDANX0F72+qpEAqxrD5YCexdhv+/WeO3YoEECgqRhCLbG71OzREfN2lrgl7vG
pqTAbUDJK2RxL4yeARU9WcHT2mXplK5w0w63IdgM8kQdodEPHTlP//lafUDq87Pj
rcTYeUehLBvtclbEo9bmmnN4JOGNMywVXCw2lQIDAQABAoIBAFzg9uwSg2iDK9dP
4ndiGuynKD4nOj8P8oZRsYGHZACFVVyjsR/f79l7iBPCNzkeHoucQJ1d/p2dS10S
C1u5KOenv0Ua6ruyb5mwiSOIX4sPeckjbHUAI/AgQ7Vy+YZId6KfByFutvkdHOTa
Tk0xNjpakUGgFpBF/S82QaGnLCxWtdSvuIZTzhC9bQGL+7TjgZknTqZUhYHLbgH3
XUBLV/Zavce77DJ02YtcZL9UphlWbuZuOF1RESn3Rk7MM3rzLTpjrDzp+EWM9T0H
4B1Zj4PIlVGdjEwUzHfK39KQYOGqhZE6O6Z8mm9H1V3+EaoCjFV6Nt3HwAXvJttc
/K/HykECgYEAwg+zCnsPlfI0FuT5W7Fi4bLSRV1IxW/BueR3ct2KUVqieP9DzmZB
NEI3ibn+/1MoUjyjAMROq8YBQ/oSpjvez/SqFbJ3xH1zQtAwhcO+3wU1GwftAah5
ZAtSJYRd6AQr1kaj+P5ZEqdxI9MJEPzsOR0eRKiPLVLF+OoDjpb0hkUCgYEA03Ap
mjYXiVSzo0NVEcP7f4k+t2Wwoms7O4xZfLuSmvNjhrZmukuu3TIYaCbW8x4wyBe/
Vfe8W4HFuu5IyrHXt/7BYWtSFlKsUyc5sveSktAXuVnZePlowm/NPjJ0EE38I0WV
aHWRlUW4H8j9ghwLKlea77+nfY/Q+pba8Ccc3BECgYEAqJD4hY8Vn7sOUiC9FU/F
Q6WQDp6UGqQT1ARHWahkgHxJCu84l+2sj9dA5MqCXIiASsbPFFhwuba53LE5R9pT
lbHBmC046Z3K4+txao/4mUKtuXguADW2lBddWKdc5q/Q4ETmI9/TwWde2K50fqQk
EQxhAWSlUcpHmwqy4kXvyz0CgYBtRQDrBlthiJmRnUGAfeUigv4bb306Yupomt7A
XHumgnQD8Y3jZyuGetYsNS5O1GJndgZW2kHIlKdoNK7/uar/FrQ/sWPpz23pR1NF
Tza7krk/+9Qs9dAS9A6AvzhGGNdeLx7IrkG/gBloq8l/jRikGEQk9MoNVN6uMnoR
NFVw0QKBgH2RW41bzJOJcWnArZR/qp6RT23SQeujOMcRGfH25jpoXU8fKup1Npt8
MnMxUAuP09HIovhn841Y7p+hlh4gSpsvYjLfgX0jyzJPhOmtBu0vEY7fLN6kQLiW
RRoQIlr5T8PG4vXwsn2/hohILCJJyHAee/4gIq42jLu6hQsQxcoy
-----END RSA PRIVATE KEY-----
	`

	opts := &IstioCAOptions{
		SigningCertBytes: []byte(signingCert),
		SigningKeyBytes:  []byte(signingKey),
		RootCertBytes:    []byte(rootCert),
	}

	ca, err := NewIstioCA(opts)
	if ca != nil || err == nil {
		t.Errorf("Expecting an error but an Istio CA is wrongly instantiated")
	}

	errMsg := "invalid parameters: cannot verify the signing cert with the provided root chain and cert pool"
	if err.Error() != errMsg {
		t.Errorf("Unexpected error message: expecting '%s' but the actual is '%s'", errMsg, err.Error())
	}
}

func TestSignCSR(t *testing.T) {
	host := "spiffe://example.com/ns/foo/sa/bar"
	opts := CertOptions{
		Host:       host,
		Org:        "istio.io",
		RSAKeySize: 512,
	}
	csrPEM, keyPEM, err := GenCSR(opts)
	if err != nil {
		t.Error(err)
	}

	ca, err := createCA()
	if err != nil {
		t.Error(err)
	}

	certPEM, err := ca.Sign(csrPEM)
	if err != nil {
		t.Error(err)
	}

	fields := &testutil.VerifyFields{
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	if err = testutil.VerifyCertificate(keyPEM, certPEM, ca.GetRootCertificate(), host, fields); err != nil {
		t.Error(err)
	}

	cert, err := pki.ParsePemEncodedCertificate(certPEM)
	if err != nil {
		t.Error(err)
	}

	san := pki.ExtractSANExtension(cert.Extensions)
	if san == nil {
		t.Errorf("No SAN extension is found in the certificate")
	}
	expected := buildSubjectAltNameExtension(host)
	if !reflect.DeepEqual(expected, san) {
		t.Errorf("Unexpected extensions: wanted %v but got %v", expected, san)
	}
}

func createCA() (CertificateAuthority, error) {
	start := time.Now().Add(-5 * time.Minute)
	end := start.Add(24 * time.Hour)

	// Generate root CA key and cert.
	rootCAOpts := CertOptions{
		IsCA:         true,
		IsSelfSigned: true,
		NotAfter:     end,
		NotBefore:    start,
		Org:          "Root CA",
		RSAKeySize:   1024,
	}
	rootCertBytes, rootKeyBytes := GenCert(rootCAOpts)

	rootCert, err := pki.ParsePemEncodedCertificate(rootCertBytes)
	if err != nil {
		return nil, err
	}

	rootKey, err := pki.ParsePemEncodedKey(rootKeyBytes)
	if err != nil {
		return nil, err
	}

	intermediateCAOpts := CertOptions{
		IsCA:         true,
		IsSelfSigned: false,
		NotAfter:     end,
		NotBefore:    start,
		Org:          "Intermediate CA",
		RSAKeySize:   1024,
		SignerCert:   rootCert,
		SignerPriv:   rootKey,
	}
	intermediateCert, intermediateKey := GenCert(intermediateCAOpts)

	caOpts := &IstioCAOptions{
		CertChainBytes:   intermediateCert,
		CertTTL:          time.Hour,
		SigningCertBytes: intermediateCert,
		SigningKeyBytes:  intermediateKey,
		RootCertBytes:    rootCertBytes,
	}

	return NewIstioCA(caOpts)
}
