// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package ra

import (
	"crypto/x509/pkix"
	"encoding/asn1"
	"testing"
)

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
				Id:    oidBasicConstraints,
				Value: []byte("bad value"),
			},
		},
	}

	for id, tc := range testCases {
		if _, err := extractIsCAFromBasicConstraints(tc.ext); err == nil {
			t.Errorf("%v: Expecting error to be returned but got nil", id)
		}
	}
}

func TestExtractBasicConstraintsExtension(t *testing.T) {
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
				{Id: asn1.ObjectIdentifier{2, 5, 29, 19}},
				{Id: asn1.ObjectIdentifier{3, 2, 1}},
			},
			found: true,
		},
	}

	for id, tc := range testCases {
		found := extractBasicConstraintsExtension(tc.exts) != nil
		if found != tc.found {
			t.Errorf("Case %q: expect `found` to be %t but got %t", id, tc.found, found)
		}
	}
}

func TestCheckIsCAExtension(t *testing.T) {
	caTrueExt, err := marshalBasicConstraints(true)
	if err != nil {
		t.Fatal(err)
	}
	caFalseExt, err := marshalBasicConstraints(false)
	if err != nil {
		t.Fatal(err)
	}

	testCases := map[string]struct {
		exts           []pkix.Extension
		expectedIsCA   bool
		expectedErrMsg string
	}{
		"Empty extension list": {
			exts:           []pkix.Extension{},
			expectedIsCA:   false,
			expectedErrMsg: "the BasicConstraints extension does not exist",
		},
		"Extensions without SAN": {
			exts: []pkix.Extension{
				{Id: asn1.ObjectIdentifier{1, 2, 3, 4}},
				{Id: asn1.ObjectIdentifier{3, 2, 1}},
			},
			expectedIsCA:   false,
			expectedErrMsg: "the BasicConstraints extension does not exist",
		},
		"Extensions with bad BasicConstraints value": {
			exts: []pkix.Extension{
				{Id: asn1.ObjectIdentifier{2, 5, 29, 19}, Value: []byte("bad san bytes")},
			},
			expectedIsCA:   false,
			expectedErrMsg: "failed to extract CA value from BasicConstraints extension (error asn1: syntax error: data truncated)",
		},
		"Extensions with incorrectly encoded BasicConstraints": {
			exts: []pkix.Extension{
				{Id: asn1.ObjectIdentifier{2, 5, 29, 19}, Value: append(copyBytes(caFalseExt.Value), 'x')},
			},
			expectedIsCA:   false,
			expectedErrMsg: "failed to extract CA value from BasicConstraints extension (error the SAN extension is incorrectly encoded)",
		},
		"Extensions with BasicConstraints and false CA": {
			exts: []pkix.Extension{
				{Id: asn1.ObjectIdentifier{1, 2, 3, 4}},
				*&caFalseExt,
				{Id: asn1.ObjectIdentifier{3, 2, 1}},
			},
			expectedIsCA: false,
		},
		"Extensions with BasicConstraints and true CA": {
			exts: []pkix.Extension{
				{Id: asn1.ObjectIdentifier{1, 2, 3, 4}},
				*&caTrueExt,
				{Id: asn1.ObjectIdentifier{3, 2, 1}},
			},
			expectedIsCA: true,
		},
	}

	for id, tc := range testCases {
		isCA, err := checkIsCAExtension(tc.exts)
		if isCA != tc.expectedIsCA {
			t.Errorf("Case %q: expect `isCA` to be %v but got %v", id, tc.expectedIsCA, isCA)
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

func copyBytes(src []byte) []byte {
	bs := make([]byte, len(src))
	copy(bs, src)
	return bs
}
