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

package grpc

import (
	"crypto/x509/pkix"
	"encoding/asn1"
	"reflect"
	"testing"

	"istio.io/auth/pkg/pki"
)

func TestExtractIDs(t *testing.T) {
	id := "test.id"
	sanExt, err := pki.BuildSANExtension([]pki.Identity{
		{Type: pki.TypeURI, Value: []byte(id)},
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
		actualIDs := extractIDs(tc.exts)
		if !reflect.DeepEqual(actualIDs, tc.expectedIDs) {
			t.Errorf("Case %q: unexpected identities: want %v but got %v", id, tc.expectedIDs, actualIDs)
		}
	}
}
