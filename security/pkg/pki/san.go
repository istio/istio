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
	"fmt"
)

// IdentityType represents type of an identity. This is used to properly encode
// an identity into a SAN extension.
type IdentityType int

const (
	// TypeDNS represents a DNS name.
	TypeDNS IdentityType = iota
	// TypeIP represents an IP address.
	TypeIP
	// TypeURI represents a universal resource identifier.
	TypeURI
)

var (
	// Mapping from the type of an identity to the OID tag value for the X.509
	// SAN field (see https://tools.ietf.org/html/rfc5280#appendix-A.2)
	//
	// SubjectAltName ::= GeneralNames
	//
	// GeneralNames ::= SEQUENCE SIZE (1..MAX) OF GeneralName
	//
	// GeneralName ::= CHOICE {
	//      dNSName                         [2]     IA5String,
	//      uniformResourceIdentifier       [6]     IA5String,
	//      iPAddress                       [7]     OCTET STRING,
	// }
	oidTagMap = map[IdentityType]int{
		TypeDNS: 2,
		TypeURI: 6,
		TypeIP:  7,
	}

	// A reversed map that maps from an OID tag to the corresponding identity
	// type.
	identityTypeMap = generateReversedMap(oidTagMap)

	// The OID for the SAN extension (See
	// http://www.alvestrand.no/objectid/2.5.29.17.html).
	oidSubjectAlternativeName = asn1.ObjectIdentifier{2, 5, 29, 17}
)

// Identity is an object holding both the encoded identifier bytes as well as
// the type of the identity.
type Identity struct {
	Type  IdentityType
	Value []byte
}

// BuildSANExtension builds a `pkix.Extension` of type "Subject
// Alternative Name" based on the given identities.
func BuildSANExtension(identites []Identity) (*pkix.Extension, error) {
	rawValues := []asn1.RawValue{}
	for _, i := range identites {
		tag, ok := oidTagMap[i.Type]
		if !ok {
			return nil, fmt.Errorf("unsupported identity type: %v", i.Type)
		}

		rawValues = append(rawValues, asn1.RawValue{
			Bytes: i.Value,
			Class: asn1.ClassContextSpecific,
			Tag:   tag,
		})
	}

	bs, err := asn1.Marshal(rawValues)
	if err != nil {
		return nil, fmt.Errorf("Failed to marshal the raw values for SAN field (err: %s)", err)
	}

	return &pkix.Extension{Id: oidSubjectAlternativeName, Value: bs}, nil
}

// ExtractIDsFromSAN takes a SAN extension and extracts the identities.
// The logic is mostly borrowed from
// https://github.com/golang/go/blob/master/src/crypto/x509/x509.go, with the
// addition of supporting extracting URIs.
func ExtractIDsFromSAN(sanExt *pkix.Extension) ([]Identity, error) {
	if !sanExt.Id.Equal(oidSubjectAlternativeName) {
		return nil, fmt.Errorf("The input is not a SAN extension")
	}

	var sequence asn1.RawValue
	if rest, err := asn1.Unmarshal(sanExt.Value, &sequence); err != nil {
		return nil, err
	} else if len(rest) != 0 {
		return nil, fmt.Errorf("The SAN extension is incorrectly encoded")
	}

	// Check the rawValue is a sequence.
	if !sequence.IsCompound || sequence.Tag != asn1.TagSequence || sequence.Class != asn1.ClassUniversal {
		return nil, fmt.Errorf("The SAN extension is incorrectly encoded")
	}

	ids := []Identity{}
	for bytes := sequence.Bytes; len(bytes) > 0; {
		var rawValue asn1.RawValue
		var err error

		bytes, err = asn1.Unmarshal(bytes, &rawValue)
		if err != nil {
			return nil, err
		}
		ids = append(ids, Identity{Type: identityTypeMap[rawValue.Tag], Value: rawValue.Bytes})
	}

	return ids, nil
}

// ExtractSANExtension extracts the "Subject Alternative Name" externsion from
// the given PKIX extension set.
func ExtractSANExtension(exts []pkix.Extension) *pkix.Extension {
	for _, ext := range exts {
		if ext.Id.Equal(oidSubjectAlternativeName) {
			// We don't need to examine other extensions anymore since a certificate
			// must not include more than one instance of a particular extension. See
			// https://tools.ietf.org/html/rfc5280#section-4.2.
			return &ext
		}
	}
	return nil
}

// ExtractIDs first finds the SAN extension from the given extension set, then
// extract identities from the SAN extension.
func ExtractIDs(exts []pkix.Extension) ([]string, error) {
	sanExt := ExtractSANExtension(exts)
	if sanExt == nil {
		return nil, fmt.Errorf("the SAN extension does not exist")
	}

	idsWithType, err := ExtractIDsFromSAN(sanExt)
	if err != nil {
		return nil, fmt.Errorf("failed to extract identities from SAN extension (error %v)", err)
	}

	ids := []string{}
	for _, id := range idsWithType {
		ids = append(ids, string(id.Value))
	}
	return ids, nil
}

func generateReversedMap(m map[IdentityType]int) map[int]IdentityType {
	reversed := make(map[int]IdentityType)
	for key, value := range m {
		reversed[value] = key
	}
	return reversed
}
