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

package pki

import (
	"crypto/x509/pkix"
	"encoding/asn1"
	"reflect"
	"testing"
)

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
}

func TestBuildSANExtensionWithError(t *testing.T) {
	id := Identity{Type: 10}
	if _, err := BuildSANExtension([]Identity{id}); err == nil {
		t.Error("Expecting error to be returned by got nil")
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
			t.Errorf("%v: Expecting error to be returned by got nil", id)
		}
	}
}

func TestExtractIDsFromSANWithBadEncoding(t *testing.T) {
	ext := &pkix.Extension{
		Id:    oidSubjectAlternativeName,
		Value: []byte("bad value"),
	}

	if _, err := ExtractIDsFromSAN(ext); err == nil {
		t.Error("Expecting error to be returned by got nil")
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
		exts        []pkix.Extension
		expectedIDs []string
	}{
		"Empty extension list": {
			exts:        []pkix.Extension{},
			expectedIDs: nil,
		},
		"Extensions without SAN": {
			exts: []pkix.Extension{
				{Id: asn1.ObjectIdentifier{1, 2, 3, 4}},
				{Id: asn1.ObjectIdentifier{3, 2, 1}},
			},
			expectedIDs: nil,
		},
		"Extensions with bad SAN": {
			exts: []pkix.Extension{
				{Id: asn1.ObjectIdentifier{2, 5, 29, 17}, Value: []byte("bad san bytes")},
			},
			expectedIDs: nil,
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
		actualIDs := ExtractIDs(tc.exts)
		if !reflect.DeepEqual(actualIDs, tc.expectedIDs) {
			t.Errorf("Case %q: unexpected identities: want %v but got %v", id, tc.expectedIDs, actualIDs)
		}
	}
}
