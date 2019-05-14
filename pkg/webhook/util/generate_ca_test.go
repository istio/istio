// Copyright 2019 Istio Authors
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
	"testing"
	"time"

	"istio.io/istio/pkg/webhook/model"
)

func TestGenerateKeyAndCert(t *testing.T) {
	// Options to generate a Cert.
	args := &model.Args{
		Org:              "MyOrg",
		RequestedCertTTL: 365 * 24 * time.Hour,
		RSAKeySize:       2048,
		IsSelfSigned:     true,
		CertFile:         "/etc/certs/cert-chain.pem",
		CertChainFile:    "/etc/certs/cert-chain.pem",
		KeyFile:          "/etc/certs/key.pem",
		RootCertFile:     "/etc/certs/root-cert.pem",
	}

	rootCert, chain, _, err := GenerateKeyAndCert(args, "test", "test-ns")
	if err != nil {
		t.Errorf("failed to generate certificate and key")
	}

	cert, err := ParsePemEncodedCertificate(rootCert)
	if err != nil {
		t.Errorf("failed to parse cert")
	}
	if err = cert.CheckSignatureFrom(cert); err != nil {
		t.Errorf("cert signature is invalid")
	}

	if cert.Subject.Organization[0] != "MyOrg" {
		t.Errorf("cert subject %v does not match", cert.Subject)
	}
	if cert.Subject.CommonName != "test_a" {
		t.Errorf("cert host does not match")
	}

	serverCert, err := ParsePemEncodedCertificate(chain)
	if err != nil {
		t.Errorf("failed to parse cert")
	}

	if serverCert.Subject.Organization[0] != "MyOrg" {
		t.Errorf("cert subject %v does not match", cert.Subject)
	}
	if serverCert.Subject.CommonName != "test.test-ns.svc" {
		t.Errorf("cert host does not match")
	}

}
