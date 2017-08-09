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
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"reflect"
	"testing"
)

const (
	csr = `
-----BEGIN CERTIFICATE REQUEST-----
MIIBoTCCAQoCAQAwEzERMA8GA1UEChMISnVqdSBvcmcwgZ8wDQYJKoZIhvcNAQEB
BQADgY0AMIGJAoGBANFf06eqiDx0+qD/xBAR5aMwwgaBOn6TPfSy96vOxLTsfkTg
ir/vb8UG+F5hO6yxF+z2BgzD8LwcbKnxahoPq/aWGLw3Umcqm4wxgWKHxvtYSQDG
w4zpmKOqgkagxbx32JXDlMpi6adUVHNvB838CiUys6IkVB0obGHnre8zmCLdAgMB
AAGgTjBMBgkqhkiG9w0BCQ4xPzA9MDsGA1UdEQQ0MDKGMHNwaWZmZTovL3Rlc3Qu
Y29tL25hbWVzcGFjZS9ucy9zZXJ2aWNlYWNjb3VudC9zYTANBgkqhkiG9w0BAQsF
AAOBgQCw9dL6xRQSjdYKt7exqlTJliuNEhw/xDVGlNUbDZnT0uL3zXI//Z8tsejn
8IFzrDtm0Z2j4BmBzNMvYBKL/4JPZ8DFywOyQqTYnGtHIkt41CNjGfqJRk8pIqVC
hKldzzeCKNgztEvsUKVqltFZ3ZYnkj/8/Cg8zUtTkOhHOjvuig==
-----END CERTIFICATE REQUEST-----`
	keyECDSA = `
-----BEGIN EC PARAMETERS-----
MGgCAQEEHBMUyVWFKTW4TwtwCmIAxdpsBFn0MV7tGeSA32CgBwYFK4EEACGhPAM6
AATCkAx7whb2k3xWm+UjlFWFiV11oYmIdYgXqiAQkiz7fEq6QFhsjjCizeGzAlhT
TmngRSxv/dSvGA==
-----END EC PARAMETERS-----`

	keyRSA = `
-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEAw/OBAAhDu58f0HkJlJBtb42Jp9EECC+WYEOVEdM/Y9fqcoSF
b19NxztVqy0r/aW8pCO3DZ2EYIA3Y9pYasDfhsIl9lhQkvEwk/05iL6oNrZ45Bgs
iSK+R5OlO9pXtj6HF948qFTDYbYVqki3rAWSSYeGpQ+/s/xcIIIKH5ozKs7DTqR8
svQ6t7Hxg0vYSUCHfJo25yIvoo8XGZxrFWOZDXfHHC22q8kuuxT82bdQo7KzYhgn
uujyzIZYqgG9BuUmB6UYdvuDRRDz4HDfERSFFxZbTAaMPNgCRvQnkPS0DJO0XZW2
T9m3bQvaqTgFI/capuhhgRcP0UrStJKZO7LVHQIDAQABAoIBAFLw0v2Mgf78j57S
XLfBmlDJfCbIVgiQ+/mrIYH2BLLiRZ5LcZ9+m5FlEBHwgNpQONTROT5OGiYun0No
vFwTX4nOy/rFzvUjmghJ+vxilxjxi6IgiVlSl2/8ksgO12mQdeYob0xg9IJ7bBgz
x2rMwOrWrqtXSzGH9AbehCJ0RowrUUjTujnow8WrDVS0cjPIl9c1eQDIDlHCUskC
iGMYYfJtB1nkdw1Kkp1YVmCYqwzVENi6+Hx66j/oOtGteTelwFmclc7JIK7liKEZ
xnDbgVTkIp9nSszpHStWikwh/srCWI7trC/k0viViZQLOd/64CyJ/sf8uZDzsG7f
hoiK3pECgYEAxF/d7QcDWaXCBkw4yC8ozqp6KTcA8thVuBK0Adtpzuf9t8h+B7V2
wlkSEs4A3YiUnxolEh1XOT+0u3uGfawlhFgXxEEHeBq+Lz6PIbWuVSl4PiWo8vtj
9MoBYRPtJelhHkBfjunqqaFwdRQQvXjmCsQfx4UAhBxdXvc2kTR/y08CgYEA/3K8
DKXldQliScqkG+Acj6nNgugecLYjCHgmAglHX6jwDuTVqNF3cv3DF5bZ4/nIbDsk
WooVhS4AEFYWceqmTveGsJNuDMoRSWNwDFRBu5Iq6LxneKiXp9vuZ4U4xZNejrgx
la7w1hQs92qCloY4Jxw9Ls3zKub4vC26CfJwzdMCgYBGw0T1ZNGQPGruWgkcGeJa
lpPuxiNRXyOEcTjscmRuaqrCzzybCokA/5fDrvgg3Fax/nndTTVhK9O0u457Os1K
I3RtBAHtBbYC0EhDnXR0u7zYqDl5VZ1vWFum38dVIgQdIpVMqn4lIkej6Ncfb7F1
r7bD7umAsbfzwKGpMYHbgQKBgQDL03vzR6hIc71mjffWekOv6lieXKJ1Yw+fIWeK
dmbqEH3EFJnbg5AhRBSYTPj9bICcw7AlQksbonHQlzB/ozEij2V8nZbRQ6b5fQuZ
+t0cUuxEGpkhcLzZ5qZbGbUMCaQIkzaVbiqjVyPuI6Ghg+VoZ6L2JsUh9XyBgqcQ
as/RmwKBgGPB8PHYHyz0km8LxM/GPstcoO4Ls5coS3MX2EBDKGqWOIOtLKz0azc7
R4beF5BJE6ulhLig4fkOWH4CIvw2Y1/22GJE/fYjUTRMD57ZdYuKqSyMNxwqiolw
xGSDfnFvR13RCqeUdlQofVYpolqrSobOyOVfQv2ksnPPsC87NISM
-----END RSA PRIVATE KEY-----`

	certRSA = `
-----BEGIN CERTIFICATE-----
MIIC+zCCAeOgAwIBAgIQQ0vFSayWg4FQBBr1EpI5rzANBgkqhkiG9w0BAQsFADAT
MREwDwYDVQQKEwhKdWp1IG9yZzAeFw0xNzAzMTEwNjA0MDJaFw0xODAzMTEwNjA0
MDJaMBMxETAPBgNVBAoTCEp1anUgb3JnMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A
MIIBCgKCAQEAw/OBAAhDu58f0HkJlJBtb42Jp9EECC+WYEOVEdM/Y9fqcoSFb19N
xztVqy0r/aW8pCO3DZ2EYIA3Y9pYasDfhsIl9lhQkvEwk/05iL6oNrZ45BgsiSK+
R5OlO9pXtj6HF948qFTDYbYVqki3rAWSSYeGpQ+/s/xcIIIKH5ozKs7DTqR8svQ6
t7Hxg0vYSUCHfJo25yIvoo8XGZxrFWOZDXfHHC22q8kuuxT82bdQo7KzYhgnuujy
zIZYqgG9BuUmB6UYdvuDRRDz4HDfERSFFxZbTAaMPNgCRvQnkPS0DJO0XZW2T9m3
bQvaqTgFI/capuhhgRcP0UrStJKZO7LVHQIDAQABo0swSTAOBgNVHQ8BAf8EBAMC
BaAwEwYDVR0lBAwwCgYIKwYBBQUHAwEwDAYDVR0TAQH/BAIwADAUBgNVHREEDTAL
gglsb2NhbGhvc3QwDQYJKoZIhvcNAQELBQADggEBAITDuqOhN1jwiA72qSWzOwuy
bMHPkUTUw2JfICtPS0AlfNNVJXREUi4KoX81ju126PGQeOTApWvS5Kkd6PbNqVH9
g3myAKrkyjewTfFtK5OOOQGzQT6lCEhKdZJusdqfAMl1heFJGnZ6GAi38ftdz2Z8
0LPyyIaVBvexNnTPrqoBqdtWyzjYIdMnsSNWJnldmWjwA76sW+vvlLvTONiT4unM
8ia4GGIw7GK4E/7qxl27q6pXdZkZgG53XItYiUJGAKeBJ2nQfXq0qSmtpHkF17Cu
hw25X3FJpzRq62JxTx5q6+M2c07g4dkbfMDp/TO7vF4SWruU6JBZj5MVDYn4PEA=
-----END CERTIFICATE-----`

	certECDSA = `
-----BEGIN CERTIFICATE-----
MIIBSzCB+qADAgECAhAzJszEACNBOHrsfSUJMPsHMAoGCCqGSM49BAMCMAsxCTAH
BgNVBAoTADAeFw0xNzAzMTMwNTE2NThaFw0xNzAzMTMwNTE2NThaMAsxCTAHBgNV
BAoTADBOMBAGByqGSM49AgEGBSuBBAAhAzoABMKQDHvCFvaTfFab5SOUVYWJXXWh
iYh1iBeqIBCSLPt8SrpAWGyOMKLN4bMCWFNOaeBFLG/91K8Yo0swSTAOBgNVHQ8B
Af8EBAMCBaAwEwYDVR0lBAwwCgYIKwYBBQUHAwEwDAYDVR0TAQH/BAIwADAUBgNV
HREEDTALgglsb2NhbGhvc3QwCgYIKoZIzj0EAwIDQAAwPQIcY8lgBAAtFWtxmk9k
BB6nORpwdv4LVt/BFgLwWQIdAKvHn7cxBJ+aAC25rIumRNKDzP7PkV0HDbxtX+M=
-----END CERTIFICATE-----`
)

func TestParsePemEncodedCertificate(t *testing.T) {
	testCases := map[string]struct {
		errMsg        string
		pem           string
		publicKeyAlgo x509.PublicKeyAlgorithm
	}{
		"Invalid PEM string": {
			errMsg: "Invalid PEM encoded certificate",
			pem:    "invalid pem string",
		},
		"Invalid certificate string": {
			errMsg: "Failed to parse X.509 certificate",
			pem:    keyECDSA,
		},
		"Parse RSA certificate": {
			publicKeyAlgo: x509.RSA,
			pem:           certRSA,
		},
		"Parse ECDSA cert": {
			pem:           certECDSA,
			publicKeyAlgo: x509.ECDSA,
		},
	}

	for id, c := range testCases {
		cert, err := ParsePemEncodedCertificate([]byte(c.pem))
		if c.errMsg != "" {
			if err == nil {
				t.Errorf("%s: no error is returned", id)
			} else if c.errMsg != err.Error() {
				t.Errorf("%s: Unexpected error message: want %s but got %s", id, c.errMsg, err.Error())
			}
		} else if cert.PublicKeyAlgorithm != c.publicKeyAlgo {
			t.Errorf("%s: Unexpected public key algorithm: want %d but got %d", id, c.publicKeyAlgo, cert.PublicKeyAlgorithm)
		}
	}
}

func TestParsePemEncodedCSR(t *testing.T) {
	testCases := map[string]struct {
		algo   x509.PublicKeyAlgorithm
		errMsg string
		pem    string
	}{
		"Invalid PEM string": {
			errMsg: "Certificate signing request is not properly encoded",
			pem:    "bad pem string",
		},
		"Invalid CSR string": {
			errMsg: "Failed to parse X.509 certificate signing request",
			pem:    certECDSA,
		},
		"Parse CSR": {
			algo: x509.RSA,
			pem:  csr,
		},
	}

	for id, c := range testCases {
		_, err := ParsePemEncodedCSR([]byte(c.pem))
		if c.errMsg != "" {
			if err == nil {
				t.Errorf("%s: no error is returned", id)
			} else if c.errMsg != err.Error() {
				t.Errorf("%s: Unexpected error message: want %s but got %s", id, c.errMsg, err.Error())
			}
		}
	}
}

func TestParsePemEncodedKey(t *testing.T) {
	testCases := map[string]struct {
		errMsg  string
		pem     string
		keyType reflect.Type
	}{
		"Invalid PEM string": {
			errMsg: "Invalid PEM-encoded key",
			pem:    "Invalid PEM string",
		},
		"Invalid PEM block type": {
			errMsg: "Unsupported PEM block type for a private key: CERTIFICATE",
			pem:    certRSA,
		},
		"Parse RSA key": {
			pem:     keyRSA,
			keyType: reflect.TypeOf(&rsa.PrivateKey{}),
		},
		"Parse invalid RSA key": {
			pem: `
-----BEGIN RSA PRIVATE KEY-----
-----END RSA PRIVATE KEY-----`,
			errMsg: "Failed to parse the RSA private key",
		},
		"Parse ECDSA key": {
			pem:     keyECDSA,
			keyType: reflect.TypeOf(&ecdsa.PrivateKey{}),
		},
		"Parse invalid ECDSA key": {
			pem: `
-----BEGIN EC PARAMETERS-----
-----END EC PARAMETERS-----`,
			errMsg: "Failed to parse the ECDSA private key",
		},
	}

	for id, c := range testCases {
		key, err := ParsePemEncodedKey([]byte(c.pem))
		if c.errMsg != "" {
			if err == nil {
				t.Errorf("%s: no error is returned", id)
			} else if c.errMsg != err.Error() {
				t.Errorf(`%s: Unexpected error message: want "%s" but got "%s"`, id, c.errMsg, err.Error())
			}
		} else if keyType := reflect.TypeOf(key); keyType != c.keyType {
			t.Errorf("%s: Unmatched key type: expected %v but got %v", id, c.keyType, keyType)
		}
	}
}
