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

// Package testcert holds test-only TLS credentials for status server tests.
// These credentials are self-signed and must never be used outside of tests.
package testcert

// TestCertKey is the PEM-encoded RSA private key for the test TLS certificate.
// It is stored here (rather than as a raw .key file) to avoid committing a
// bare private-key file to the repository.
var TestCertKey = []byte(`-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEAzxAYtpWvX2SXWqoirXS10RYMDysuKFvJzItpMNBbNEazY+hO
/tE3f9dhHJFGCQ6jlmOIuyURzdm79TWMfWu3Iea1bwXjUtqLZ9+Pz6852Nmyj/OZ
xWxMCnNPNPDHVkkw3z9/H1Cz3pXLqczG7eAkY0clDoLJwcwMe+Ywa/S+v9JJoUYD
5PhR6zH9qygnNUOsFrcX4mPaD/zflWkSmZ6K1x0k9FBhFoF5nYdqsZ/DhKMR861O
/QLo8xk7cFxOk2IGQzvd60HfaJTRiLNyDWOlt5vx+ItXP0RNWqe87u69dIn9hhJz
r9M+KDPqTEMIHcAztMbznZ/fg1UvQZRdmoeMMQIDAQABAoIBAFsGTnblcoTS6Z5X
sIrkBZF2ybJZXx8qypl6p7FnxtBCTFYdJ6zpOCag/fXa/xi4ML3J36+1ahA+KVxw
P+Ra19S1YQj/Y6FmpWXyZ3v7IcjsWozhn7WkGAF4E1fIiTirUCqz9SRFC+1LmI56
kPC9WgGyot2wLRVeqBZHaP3sR3Z3Jo0ed9jm0z2XmR6R2/UO6QOY4otGj9kYwh6a
wq5VKRxljyH2dAJKrS5H2pwXWxdxlx3QiIuJgC7eyAKIlUjccfP20Z4DvCz8DA/g
d1S87yF6jc/+tmcHFmysSYWVQOgq9cmWFnElTjXWTVRGhX8YD4YyC7MLlO9adgNw
UdaOgAECgYEA7p47X4NKPj2vM3K0d49lCn8gcCb3HkhZof0lLQAB4lK8mv70ErDC
/p9NAKRxLIjzFoRQ+UN0bMewrtKL8i5ojZl8MRo2QUYeCYqlPHnftWflr8tQUBHX
/0LjM7n6fuODy9Do1JcyQEOurw+4uPgKpSuZy2UY+krdsGVMT7POCDECgYEA3iVr
qp//wQAkM/7hhxpNMBAHDzJubfqQ0YN28XrDCDicXMBwvmxLssUHqx0kJbe0eTEy
3tmUovGkYOL5ycQOFESDydkVerEPtuEHn4CIb6QqWVQFdfxXqMiSNH2iEGs1GAXY
xzaqRjaDmpN2aDDfq4ThJh0zWescwmS6mUa0xAECgYAyPPI3K8cnz4jhhhbkzTXy
vc0wj6ObppPofQmkrcm3wr+eymrMvJZxUUy/A+AoBjVX2kfKEx+h/3D9faqlNIwi
s9vn4qLln0OXsq8TSn2FDfjXyDCCix80yPpY26EXsgL/mF5M1ABqc1WF2gOEPgTP
vZxFrGVT3QtLpigo56xLIQKBgQC40c9S5M0GyNRWAh+mpKZFb4BAD4g6rfXgqgzC
eY1cAKVusZjbhQQx1qU7owIY808OaXVWXRXBv2MwTIbfa+L+z8YJoDeznS5iy7Po
6yoYIDAvo6zrbaeMwFqLm17DZD6HHw4tJ/jgc6hoaXlg1BCzBdnAORkpHWgO/3kT
3vS0AQKBgQC7eoclzScXV77p7ISzbri/V4ooy5cnl+Wg4UlA8HQEQqO36rr7R7IL
jolIJFwc1GfA7RwxJZRZpFQ1ZTIy3EJ5JIW1lgrE8EZFKs8XFbnsdxpjc2p75t24
mJTjQHNgoVgVtT8kas0fPTi3Jw7DVZom2tUbCnp/zObNzU9s9yOKUQ==
-----END RSA PRIVATE KEY-----`)

// TestCert is the PEM-encoded self-signed certificate paired with TestCertKey.
var TestCert = []byte(`-----BEGIN CERTIFICATE-----
MIIDgzCCAmugAwIBAgIJAPjLZPVcFqpYMA0GCSqGSIb3DQEBCwUAMFgxCzAJBgNV
BAYTAkFVMRMwEQYDVQQIDApTb21lLVN0YXRlMSEwHwYDVQQKDBhJbnRlcm5ldCBX
aWRnaXRzIFB0eSBMdGQxETAPBgNVBAMMCHRlc3QuY29tMB4XDTE5MDMyODEwMjcx
NFoXDTI5MDMyNTEwMjcxNFowWDELMAkGA1UEBhMCQVUxEzARBgNVBAgMClNvbWUt
U3RhdGUxITAfBgNVBAoMGEludGVybmV0IFdpZGdpdHMgUHR5IEx0ZDERMA8GA1UE
AwwIdGVzdC5jb20wggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQDPEBi2
la9fZJdaqiKtdLXRFgwPKy4oW8nMi2kw0Fs0RrNj6E7+0Td/12EckUYJDqOWY4i7
JRHN2bv1NYx9a7ch5rVvBeNS2otn34/PrznY2bKP85nFbEwKc0808MdWSTDfP38f
ULPelcupzMbt4CRjRyUOgsnBzAx75jBr9L6/0kmhRgPk+FHrMf2rKCc1Q6wWtxfi
Y9oP/N+VaRKZnorXHST0UGEWgXmdh2qxn8OEoxHzrU79AujzGTtwXE6TYgZDO93r
Qd9olNGIs3INY6W3m/H4i1c/RE1ap7zu7r10if2GEnOv0z4oM+pMQwgdwDO0xvOd
n9+DVS9BlF2ah4wxAgMBAAGjUDBOMB0GA1UdDgQWBBS6ivHJJ8VYvfUcunC//A7R
VCabHjAfBgNVHSMEGDAWgBS6ivHJJ8VYvfUcunC//A7RVCabHjAMBgNVHRMEBTAD
AQH/MA0GCSqGSIb3DQEBCwUAA4IBAQA3JsxnmPrMKoNCUNWMEsMhg2MgO3ptYi+n
8UZXvMQSezRG8vOyPibHMT6w/fM+x85Oz+IpXDQVTwEjO6Pm4HSzND3FloTQl7cp
ho//oVuLeDoNw7RVUO+tM4zLja7GhhAUbpDapLWSTq7A1EAxykKKuvuCqoKJlHwg
LKLGIdrxrKFjWklAxlJ7F29+xK0RGXjPIdovsEVs6xAyxpi/Ut0kHbXghM1M8vxV
h4nTTTgpvnE2NbI8gOsUvvqJyw1XuvxjTe7kwCUp1ispXQrNUZrlf+epXw0dSQPr
umuui2OJi54voYZgZ7ByGpXWckgVz2L6cn7OoUMYLx7FB7dzZk6L
-----END CERTIFICATE-----`)
