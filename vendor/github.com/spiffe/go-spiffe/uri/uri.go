package uri

import (
	"encoding/asn1"
	"errors"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"io/ioutil"
)

func getURINamesFromSANExtension(sanExtension []byte) (uris []string, err error) {
	// RFC 5280, 4.2.1.6

	// SubjectAltName ::= GeneralNames
	//
	// GeneralNames ::= SEQUENCE SIZE (1..MAX) OF GeneralName
	//
	// GeneralName ::= CHOICE {
	//      otherName                       [0]     OtherName,
	//      rfc822Name                      [1]     IA5String,
	//      dNSName                         [2]     IA5String,
	//      x400Address                     [3]     ORAddress,
	//      directoryName                   [4]     Name,
	//      ediPartyName                    [5]     EDIPartyName,
	//      uniformResourceIdentifier       [6]     IA5String,
	//      iPAddress                       [7]     OCTET STRING,
	//      registeredID                    [8]     OBJECT IDENTIFIER }
	var seq asn1.RawValue
	var rest []byte
	if rest, err = asn1.Unmarshal(sanExtension, &seq); err != nil {
		return uris, err
	} else if len(rest) != 0 {
		err = errors.New("x509: trailing data after X.509 extension")
		return uris, err
	}
	if !seq.IsCompound || seq.Tag != 16 || seq.Class != 0 {
		err = asn1.StructuralError{Msg: "bad SAN sequence"}
		return uris, err
	}

	rest = seq.Bytes
	for len(rest) > 0 {
		var v asn1.RawValue
		rest, err = asn1.Unmarshal(rest, &v)
		if err != nil {
			return uris, err
		}
		if v.Tag == 6 {
			uris = append(uris, string(v.Bytes))
		}
	}

	return uris, err
}

// GetURINamesFromCertificate takes a parsed X.509 certificate and gets the URIs from the SAN extension.
func GetURINamesFromCertificate(cert *x509.Certificate) (uris []string, err error) {
	for _, ext := range GetExtensionsFromAsn1ObjectIdentifier(cert, OidExtensionSubjectAltName) {
		uris, err = getURINamesFromSANExtension(ext.Value)
		if err != nil {
			return uris, err
		}
	}

	return uris, nil
}

// GetURINamesFromPEM parses a PEM-encoded X.509 certificate and gets the URIs from the SAN extension.
func GetURINamesFromPEM(encodedCertificate string) (uris []string, err error) {
	return uriNamesFromPEM([]byte(encodedCertificate))
}

func uriNamesFromPEM(encodedCertificate []byte) (uris []string, err error) {
	block, _ := pem.Decode(encodedCertificate)
	if block == nil {
		return uris, errors.New("failed to decode certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return uris, errors.New("failed to parse certificate: " + err.Error())
	}

	return GetURINamesFromCertificate(cert)
}

// FGetURINamesFromPEM retrieves URIs from the SAN extension of a
// PEM-encoded X.509 certificate, whose content is in the provided io.Reader.
func FGetURINamesFromPEM(f io.Reader) (uris []string, err error) {
	blob, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}
	return uriNamesFromPEM(blob)
}

// GetURINamesFromExtensions retrieves URIs from the SAN extension of a slice of extensions
func GetURINamesFromExtensions(extensions *[]pkix.Extension) (uris []string, err error) {
	for _, ext := range *extensions {
		if ext.Id.Equal(OidExtensionSubjectAltName) {
			uris, err = getURINamesFromSANExtension(ext.Value)
			if err != nil {
				return uris, err
			}
		}
	}

	return uris, nil
}

// MarshalUriSANs takes URI strings and returns the ASN.1 structure to be used
// in the Value field for the SAN Extension
func MarshalUriSANs(uris []string) (derBytes []byte, err error) {
	var rawValues []asn1.RawValue
	for _, name := range uris {
		rawValues = append(rawValues, asn1.RawValue{Tag: 6, Class: 2, Bytes: []byte(name)})
	}

	return asn1.Marshal(rawValues)
}
