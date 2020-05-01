// Copyright 2018 Istio Authors
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

package caclient

import (
	"encoding/pem"
	"testing"
	"time"

	"golang.org/x/net/context"
	nodeagentutil "istio.io/istio/security/pkg/nodeagent/util"
	pkiutil "istio.io/istio/security/pkg/pki/util"
)

func TestCreateKeyfactorCSRRequest(t *testing.T) {

	options := pkiutil.CertOptions{
		Host:       "spiffe://cluster.local/ns/default/sa/default",
		RSAKeySize: 2048,
		Org:        "Istio Test",
		IsCA:       false,
		IsDualUse:  false,
		PKCS8Key:   false,
		TTL:        24 * time.Hour,
	}

	// Generate the cert/key, send CSR to CA.
	csrPEM, _, err := pkiutil.GenCSR(options)

	if err != nil {
		t.Errorf("Test case: failed to create CSRPem : %v", err)
	}

	cl, err := NewKeyFactorCAClient("https://kmstech.thedemodrive.com", false, "cluster.local", nil)

	certChain, errSign := cl.CSRSign(context.TODO(), "", csrPEM, "Istio", 6400)

	if errSign != nil {
		t.Errorf("CSRSign failed with errs: %v", errSign)

	}

	for _, cert := range certChain {
		if _, err := nodeagentutil.ParseCertAndGetExpiryTimestamp([]byte(cert)); err != nil {
			t.Errorf("Expect not error, but got: %v", err)
		}

		_, err := pem.Decode([]byte(cert))

		if err != nil {
			t.Errorf("Expect not error, but got: %v", err)
		}

	}

}
