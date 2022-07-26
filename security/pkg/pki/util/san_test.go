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
	"crypto/x509/pkix"
	"encoding/asn1"
	"net"
	"reflect"
	"testing"
)

func getSANExtension(identites []Identity, t *testing.T) *pkix.Extension {
	ext, err := BuildSANExtension(identites)
	if err != nil {
		t.Errorf("A unexpected error has been encountered (error: %v)", err)
	}
	return ext
}

func TestBuildSubjectAltNameExtension(t *testing.T) {
	uriIdentity := Identity{Type: TypeURI, Value: []byte("spiffe://test.domain.com/ns/default/sa/default")}
	ipIdentity := Identity{Type: TypeIP, Value: net.ParseIP("10.0.0.1").To4()}
	dnsIdentity := Identity{Type: TypeDNS, Value: []byte("test.domain.com")}

	testCases := map[string]struct {
		hosts       string
		expectedExt *pkix.Extension
	}{
		"URI host": {
			hosts:       "spiffe://test.domain.com/ns/default/sa/default",
			expectedExt: getSANExtension([]Identity{uriIdentity}, t),
		},
		"IP host": {
			hosts:       "10.0.0.1",
			expectedExt: getSANExtension([]Identity{ipIdentity}, t),
		},
		"DNS host": {
			hosts:       "test.domain.com",
			expectedExt: getSANExtension([]Identity{dnsIdentity}, t),
		},
		"URI, IP and DNS hosts": {
			hosts:       "spiffe://test.domain.com/ns/default/sa/default,10.0.0.1,test.domain.com",
			expectedExt: getSANExtension([]Identity{uriIdentity, ipIdentity, dnsIdentity}, t),
		},
	}

	for id, tc := range testCases {
		if ext, err := BuildSubjectAltNameExtension(tc.hosts); err != nil {
			t.Errorf("Case %q: a unexpected error has been encountered (error: %v)", id, err)
		} else if !reflect.DeepEqual(ext, tc.expectedExt) {
			t.Errorf("Case %q: unexpected extension returned: want %v but got %v", id, tc.expectedExt, ext)
		}
	}
}

func TestBuildAndExtractIdentities(t *testing.T) {
	ids := []Identity{
		{Type: TypeDNS, Value: []byte("test.domain.com")},
		{Type: TypeIP, Value: []byte("10.0.0.1")},
		{Type: TypeURI, Value: []byte("spiffe://test.domain.com/ns/default/sa/default")},
	}
	san, err := BuildSANExtension(ids)
	if err != nil {
		t.Errorf("A unexpected error has been encountered (error: %v)", err)
	}

	actualIds, err := ExtractIDsFromSAN(san)
	if err != nil {
		t.Errorf("A unexpected error has been encountered (error: %v)", err)
	}

	if !reflect.DeepEqual(actualIds, ids) {
		t.Errorf("Unmatched identities: before encoding: %v, after decoding %v", ids, actualIds)
	}

	if !san.Critical {
		t.Errorf("SAN field is not critical.")
	}
}

func TestBuildSANExtensionWithError(t *testing.T) {
	id := Identity{Type: 10}
	if _, err := BuildSANExtension([]Identity{id}); err == nil {
		t.Error("Expecting error to be returned but got nil")
	}
}

func TestExtractIDsFromSANWithError(t *testing.T) {
	testCases := map[string]struct {
		ext *pkix.Extension
	}{
		"Wrong OID": {
			ext: &pkix.Extension{
				Id: asn1.ObjectIdentifier{1, 2, 3},
			},
		},
		"Wrong encoding": {
			ext: &pkix.Extension{
				Id:    oidSubjectAlternativeName,
				Value: []byte("bad value"),
			},
		},
	}

	for id, tc := range testCases {
		if _, err := ExtractIDsFromSAN(tc.ext); err == nil {
			t.Errorf("%v: Expecting error to be returned but got nil", id)
		}
	}
}

func TestExtractIDsFromSANWithBadEncoding(t *testing.T) {
	ext := &pkix.Extension{
		Id:    oidSubjectAlternativeName,
		Value: []byte("bad value"),
	}

	if _, err := ExtractIDsFromSAN(ext); err == nil {
		t.Error("Expecting error to be returned but got nil")
	}
}

func TestExtractSANExtension(t *testing.T) {
	testCases := map[string]struct {
		exts  []pkix.Extension
		found bool
	}{
		"No extension": {
			exts:  []pkix.Extension{},
			found: false,
		},
		"An extensions with wrong OID": {
			exts: []pkix.Extension{
				{Id: asn1.ObjectIdentifier{1, 2, 3}},
			},
			found: false,
		},
		"Correct SAN extension": {
			exts: []pkix.Extension{
				{Id: asn1.ObjectIdentifier{1, 2, 3}},
				{Id: asn1.ObjectIdentifier{2, 5, 29, 17}},
				{Id: asn1.ObjectIdentifier{3, 2, 1}},
			},
			found: true,
		},
	}

	for id, tc := range testCases {
		found := ExtractSANExtension(tc.exts) != nil
		if found != tc.found {
			t.Errorf("Case %q: expect `found` to be %t but got %t", id, tc.found, found)
		}
	}
}

func TestExtractIDs(t *testing.T) {
	id := "test.id"
	sanExt, err := BuildSANExtension([]Identity{
		{Type: TypeURI, Value: []byte(id)},
	})
	if err != nil {
		t.Fatal(err)
	}

	testCases := map[string]struct {
		exts           []pkix.Extension
		expectedIDs    []string
		expectedErrMsg string
	}{
		"Empty extension list": {
			exts:           []pkix.Extension{},
			expectedIDs:    nil,
			expectedErrMsg: "the SAN extension does not exist",
		},
		"Extensions without SAN": {
			exts: []pkix.Extension{
				{Id: asn1.ObjectIdentifier{1, 2, 3, 4}},
				{Id: asn1.ObjectIdentifier{3, 2, 1}},
			},
			expectedIDs:    nil,
			expectedErrMsg: "the SAN extension does not exist",
		},
		"Extensions with bad SAN": {
			exts: []pkix.Extension{
				{Id: asn1.ObjectIdentifier{2, 5, 29, 17}, Value: []byte("bad san bytes")},
			},
			expectedIDs:    nil,
			expectedErrMsg: "failed to extract identities from SAN extension (error asn1: syntax error: data truncated)",
		},
		"Extensions with incorrectly encoded SAN": {
			exts: []pkix.Extension{
				{Id: asn1.ObjectIdentifier{2, 5, 29, 17}, Value: append(copyBytes(sanExt.Value), 'x')},
			},
			expectedIDs:    nil,
			expectedErrMsg: "failed to extract identities from SAN extension (error the SAN extension is incorrectly encoded)",
		},
		"Extensions with SAN": {
			exts: []pkix.Extension{
				{Id: asn1.ObjectIdentifier{1, 2, 3, 4}},
				*sanExt,
				{Id: asn1.ObjectIdentifier{3, 2, 1}},
			},
			expectedIDs: []string{id},
		},
	}

	for id, tc := range testCases {
		actualIDs, err := ExtractIDs(tc.exts)
		if !reflect.DeepEqual(actualIDs, tc.expectedIDs) {
			t.Errorf("Case %q: unexpected identities: want %v but got %v", id, tc.expectedIDs, actualIDs)
		}
		if tc.expectedErrMsg != "" {
			if err == nil {
				t.Errorf("Case %q: no error message returned: want %s", id, tc.expectedErrMsg)
			} else if tc.expectedErrMsg != err.Error() {
				t.Errorf("Case %q: unexpected error message: want %s but got %s", id, tc.expectedErrMsg, err.Error())
			}
		}
	}
}
