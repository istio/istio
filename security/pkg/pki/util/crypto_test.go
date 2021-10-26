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
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"reflect"
	"strings"
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

	keyInvalidRSA = `
-----BEGIN RSA PRIVATE KEY-----
-----END RSA PRIVATE KEY-----`

	keyECDSA = `
-----BEGIN EC PRIVATE KEY-----
MGgCAQEEHBMUyVWFKTW4TwtwCmIAxdpsBFn0MV7tGeSA32CgBwYFK4EEACGhPAM6
AATCkAx7whb2k3xWm+UjlFWFiV11oYmIdYgXqiAQkiz7fEq6QFhsjjCizeGzAlhT
TmngRSxv/dSvGA==
-----END EC PRIVATE KEY-----`

	keyInvalidECDSA = `
-----BEGIN EC PRIVATE KEY-----
-----END EC PRIVATE KEY-----`

	keyPKCS8RSA = `
-----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQC5iMw+Cvt0ZAnj
l5piKgyoFIlCAD2FeKd0CQaUA9sHk7XuWAXUAHWQrNXRIG/d9uZBedOwFQ/D8GCR
m5uQTTZC/4cww6pXvrVvX3DYe7H3hF8g3cDsHoYuR51LHzFR1lAe/y0XFaRw18Vm
WNFLnUozEeq1nT0CAyw0iXGa22ptZA3M1G9JJjDrENT6adI/N/19XH9fMiQOOOrT
zerC9DRVouXgxDsXUz2H25XT3YWu7nVs/ic57BRc3JU4JfYbVR8Br2+lAgPFvLFs
/HfX2BuvHC64sseIOVfXL7a/EaOVQhD9NKL7K+5faX21JAfN40f+qwfNLJrrUjHu
7UMpZbJVAgMBAAECggEBAJQ6RarfzUuMzRXGvjHlFF2IoqxXUs96uJYMy/OfLPNd
wIEeU/GvOD4Qx3afqqA0LHttIIHSIdlSB2TtZBiih1J5ogGEoWge1geXwalDEckF
OZchc4txS5RX5MPqtNWEGljZV6XUxZ7d1DjThssZa/lnPBRC/kXIUR3cHSYyXFHt
vA4vY4tVmH1TDUY51A64qKO5dg/Jupn1AHVnoU8eGGD31pISmlZdy64rBMdx9Ac5
u30rGP4RWR9YCSZRFtoxL2d0VoEdKAx1qN0/rPYPNUK1EIFO8kW4voNlUfyGDTLX
TdYBX5Stmm3ws6MAoJJKdpBLWmiJgqOjaiYlf/7y+zUCgYEA5mogVNiba2KRP85O
QOH2ugY1dPiiJLm6QTub7Aygy8lc6WQGa2TXBUpqGYp4rs0wjBnkhtIoErZM8cxu
ENj8dGBikfEJq95NcOMx6wsQYKRWX9JwDzNrLyUCtKK+B3kO8daHzHWTPB7mNkHF
Y91PZLmzSCdAsqxI5VHo0mzStMsCgYEAziLizDE4Q+IpNvzpGo+TxOJILTYdf1HC
OdoaWMKU1Hl6TyWGXon0/ZfAI+jxjT9sI4/Slr5ED86FT0iaIOcSgvl8PmbcvMrM
m3Nj/ZVM3EPzo8EW/LNFlZreheIuvuSwUvnzlWaWZdWJ5/z0M7Is05NsF2v5QlON
OVd/b3pVcV8CgYEAhIDTRvepqP9t9/t0FOvdLu0TIMk6tVP5QDo/WGeKsKaDv9O9
vVSoMmqwyS9QZ3WoTWk2ejGwydH8PbEKOrYNt/8VsEelACk+74Q32KrsKCdZZJFn
z9YJ9XqbK7XLAhEj/v8X6QRUP2aljN4V3XAPkCUabIvmMNnSsc2AzkG2ijECgYB6
BtTTo9928A8N6jHj81K6nmmzufFESZX8wUwPd0C7dx4cdE5S8MACzy6DE4bK4tyV
QLKdYgzQfqUUBhqXl7KxrhcKqcHKURNGgsySdSuGyQMV0VxWQ5nRslhAUWDyyFZJ
CIZVzuEBb6OvnWLCp5s5tG+sfdKUnPlhFJbv2y9xaQKBgCp1g3NtljkH1Olk+kFF
LwxpTxIxS/8jZIM6MpfcmLPyZVRrpUORgkphXvLVXub+anNqGbK0Cbc30KQGnFKD
PFsekZAmhgetPqL16MQSEZbXRSnGiklqQtew79S/yQDwZVCer8n1ABp5eZ2wsLgu
92ik2sTgTEhef6AgLeHcT5ne
-----END PRIVATE KEY-----`

	keyInvalidPKCS8 = `
-----BEGIN PRIVATE KEY-----
-----END PRIVATE KEY-----`

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
			errMsg: "invalid PEM encoded certificate",
			pem:    "invalid pem string",
		},
		"Invalid certificate string": {
			errMsg: "failed to parse X.509 certificate",
			pem:    keyECDSA,
		},
		"Parse RSA certificate": {
			publicKeyAlgo: x509.RSA,
			pem:           certRSA,
		},
		"Parse ECDSA certificate": {
			publicKeyAlgo: x509.ECDSA,
			pem:           certECDSA,
		},
	}

	for id, c := range testCases {
		cert, err := ParsePemEncodedCertificate([]byte(c.pem))
		if c.errMsg != "" {
			if err == nil {
				t.Errorf("%s: no error is returned", id)
			} else if c.errMsg != err.Error() {
				t.Errorf(`%s: Unexpected error message: expected "%s" but got "%s"`, id, c.errMsg, err.Error())
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
			errMsg: "certificate signing request is not properly encoded",
			pem:    "bad pem string",
		},
		"Invalid CSR string": {
			errMsg: "failed to parse X.509 certificate signing request",
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
				t.Errorf(`%s: no error is returned, expected "%s"`, id, c.errMsg)
			} else if c.errMsg != err.Error() {
				t.Errorf(`%s: Unexpected error message: want "%s" but got "%s"`, id, c.errMsg, err.Error())
			}
		} else if err != nil {
			t.Errorf(`%s: Unexpected error: "%s"`, id, err)
		}
	}
}

func TestParsePemEncodedKey(t *testing.T) {
	testCases := map[string]struct {
		pem     string
		keyType reflect.Type
		errMsg  string
	}{
		"Invalid PEM string": {
			pem:    "Invalid PEM string",
			errMsg: "invalid PEM-encoded key",
		},
		"Invalid PEM block type": {
			pem:    certRSA,
			errMsg: "unsupported PEM block type for a private key: CERTIFICATE",
		},
		"Parse RSA key": {
			pem:     keyRSA,
			keyType: reflect.TypeOf(&rsa.PrivateKey{}),
		},
		"Parse invalid RSA key": {
			pem:    keyInvalidRSA,
			errMsg: "failed to parse the RSA private key",
		},
		"Parse ECDSA key": {
			pem:     keyECDSA,
			keyType: reflect.TypeOf(&ecdsa.PrivateKey{}),
		},
		"Parse invalid ECDSA key": {
			pem:    keyInvalidECDSA,
			errMsg: "failed to parse the ECDSA private key",
		},
		"Parse PKCS8 key using RSA algorithm": {
			pem:     keyPKCS8RSA,
			keyType: reflect.TypeOf(&rsa.PrivateKey{}),
		},
		"Parse invalid PKCS8 key": {
			pem:    keyInvalidPKCS8,
			errMsg: "failed to parse the PKCS8 private key",
		},
	}

	for id, c := range testCases {
		key, err := ParsePemEncodedKey([]byte(c.pem))
		if c.errMsg != "" {
			if err == nil {
				t.Errorf(`%s: no error is returned, expected "%s"`, id, c.errMsg)
			} else if c.errMsg != err.Error() {
				t.Errorf(`%s: Unexpected error message: expected "%s" but got "%s"`, id, c.errMsg, err.Error())
			}
		} else if err != nil {
			t.Errorf(`%s: Unexpected error: "%s"`, id, err)
		} else if keyType := reflect.TypeOf(key); keyType != c.keyType {
			t.Errorf(`%s: Unmatched key type: expected "%v" but got "%v"`, id, c.keyType, keyType)
		}
	}
}

func TestGetRSAKeySize(t *testing.T) {
	testCases := map[string]struct {
		pem    string
		size   int
		errMsg string
	}{
		"Success with RSA key": {
			pem:  keyRSA,
			size: 2048,
		},
		"Success with PKCS8RSA key": {
			pem:  keyPKCS8RSA,
			size: 2048,
		},
		"Failure with non-RSA key": {
			pem:    keyECDSA,
			errMsg: "key type is not RSA: *ecdsa.PrivateKey",
		},
	}

	for id, c := range testCases {
		key, err := ParsePemEncodedKey([]byte(c.pem))
		if err != nil {
			t.Errorf("%s: failed to parse the Pem key.", id)
		}
		size, err := GetRSAKeySize(key)
		if c.errMsg != "" {
			if err == nil {
				t.Errorf(`%s: no error is returned, expected error: "%s"`, id, c.errMsg)
			} else if c.errMsg != err.Error() {
				t.Errorf(`%s: Unexpected error message: expected "%s" but got "%s"`, id, c.errMsg, err.Error())
			}
		} else if err != nil {
			t.Errorf(`%s: Unexpected error: "%s"`, id, err)
		} else if size != c.size {
			t.Errorf(`%s: Unmatched key size: expected %v but got "%v"`, id, c.size, size)
		}
	}
}

func TestIsSupportedECPrivateKey(t *testing.T) {
	_, ed25519PrivKey, _ := ed25519.GenerateKey(nil)
	ecdsaPrivKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	cases := map[string]struct {
		key         crypto.PrivateKey
		isSupported bool
	}{
		"ECDSA": {
			key:         ecdsaPrivKey,
			isSupported: true,
		},
		"ED25519": {
			key:         ed25519PrivKey,
			isSupported: false,
		},
	}

	for id, tc := range cases {
		if IsSupportedECPrivateKey(&tc.key) != tc.isSupported {
			t.Errorf("%s: does not match expected support level for EC signature algorithms", id)
		}
	}
}

func TestPemCertBytestoString(t *testing.T) {
	// empty check
	if len(PemCertBytestoString([]byte{})) != 0 {
		t.Errorf("Empty call fails!")
	}

	certBytes := []byte(certECDSA)
	certBytes = AppendCertByte(certBytes, []byte(certRSA))
	result := PemCertBytestoString(certBytes)
	cert1 := strings.TrimSuffix(strings.TrimPrefix(certECDSA, "\n"), "\n")
	cert2 := strings.TrimSuffix(strings.TrimPrefix(certRSA, "\n"), "\n")
	if !reflect.DeepEqual(result, []string{cert1, cert2}) {
		t.Errorf("Basic comparison fails!")
	}

	// check only first string passed if second is bogus
	certBytes = []byte(certRSA)
	certBytes = AppendCertByte(certBytes, []byte("Bogus"))
	result = PemCertBytestoString(certBytes)
	cert1 = strings.TrimSuffix(strings.TrimPrefix(certRSA, "\n"), "\n")
	if !reflect.DeepEqual(result, []string{cert1}) {
		t.Errorf("Bogus comparison fails!")
	}
}
