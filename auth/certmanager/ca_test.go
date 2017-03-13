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

package certmanager

import (
	"bytes"
	"encoding/asn1"
	"fmt"
	"testing"
)

func TestSelfSignedIstioCA(t *testing.T) {
	ca := NewSelfSignedIstioCA()
	name := "foo"
	namespace := "bar"

	cb, kb := ca.Generate(name, namespace)
	cert, _ := parsePemEncodedCertificateAndKey(cb, kb)

	foundSAN := false
	for _, ee := range cert.Extensions {
		if ee.Id.Equal(oidSubjectAltName) {
			foundSAN = true
			id := fmt.Sprintf("istio:%s.%s.cluster.local", name, namespace)
			rv := asn1.RawValue{Tag: tagURI, Class: asn1.ClassContextSpecific, Bytes: []byte(id)}
			bs, err := asn1.Marshal([]asn1.RawValue{rv})
			if err != nil {
				t.Fatal(err)
			}

			if !bytes.Equal(bs, ee.Value) {
				t.Errorf("SAN field does not match: %s is expected but actual is %s", bs, ee.Value)
			}
		}
	}
	if !foundSAN {
		t.Errorf("Generated certificate does not contain a SAN field")
	}
}
