package uri

import(
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
)

var OidExtensionSubjectAltName = asn1.ObjectIdentifier{2, 5, 29, 17}
var OidExtensionKeyUsage = asn1.ObjectIdentifier{2, 5, 29, 15}

// GetExtensionsFromAsn1ObjectIdentifier takes a parsed X.509 certificate and an OID
// and gets the extensions that match the given OID
func GetExtensionsFromAsn1ObjectIdentifier(certificate *x509.Certificate, id asn1.ObjectIdentifier) []pkix.Extension {
	var extensions []pkix.Extension

	for _, extension := range certificate.Extensions {
		if extension.Id.Equal(id) {
			extensions = append(extensions, extension)
		}
	}

	return extensions
}

// GetKeyUsageExtensionsFromCertificate takes a parsed X.509 certificate and gets the Key Usage extensions
func GetKeyUsageExtensionsFromCertificate(cert *x509.Certificate) (extension []pkix.Extension) {
	return GetExtensionsFromAsn1ObjectIdentifier(cert, OidExtensionKeyUsage)
}
