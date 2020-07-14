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

package secret

import (
	"bytes"
	"testing"

	v1 "k8s.io/api/core/v1"
)

var (
	cert1Pem = `
-----BEGIN CERTIFICATE-----
MIIC3jCCAcagAwIBAgIJAMwyWk0iqlOoMA0GCSqGSIb3DQEBCwUAMBwxGjAYBgNV
BAoMEWs4cy5jbHVzdGVyLmxvY2FsMB4XDTE4MDkyMTAyMjAzNFoXDTI4MDkxODAy
MjAzNFowHDEaMBgGA1UECgwRazhzLmNsdXN0ZXIubG9jYWwwggEiMA0GCSqGSIb3
DQEBAQUAA4IBDwAwggEKAoIBAQC8TDtfy23OKCRnkSYrKZwuHG5lOmTZgLwoFR1h
3NDTkjR9406CjnAy6Gl73CRG3zRYVgY/2dGNqTzAKRCeKZlOzBlK6Kilb0NIJ6it
s6ooMAxwXlr7jOKiSn6xbaexVMrP0VPUbCgJxQtGs3++hQ14D6WnyfdzPBZJLKbI
tVdDnAcl/FJXKVV9gIg+MM0gETWOYj5Yd8Ye0FTvoFcgs8NKkxhEZe/LeYa7XYsk
S0PymwbHwNZcfC4znp2bzu28LUmUe6kL97YU8ubvhR0muRy6h5MnQNMQrRG5Q5j4
A2+tkO0vto8gOb6/lacEUVYuQdSkMZJiqWEjWgWKeAYdkTJDAgMBAAGjIzAhMA4G
A1UdDwEB/wQEAwICBDAPBgNVHRMBAf8EBTADAQH/MA0GCSqGSIb3DQEBCwUAA4IB
AQAxWP3MT0IelJcb+e7fNTfMS0r3UhpiNkRU368Z7gJ4tDNOGRPzntW6CLnaE+3g
IjOMAE8jlXeEmNuXtDQqQoZwWc1D5ma3jyc83E5H9LJzjfmn5rAHafr29YH85Ms2
VlKdpP+teYg8Cag9u4ar/AUR4zMUEpGK5U+T9IH44lVqVH23T+DxAT+btsyuGiB0
DsM76XVDj4g3OKCUalu7a8FHvgTkBpUJBl7vwh9kqo9HwCaj4iC2CwveOm0WtSgy
K9PpVDxTGNSxqsxKn7DJQ15NTOP+gr29ABqFKwRr+S8ggw6evzHbABQTUMebaRSr
iH7cSgrzZBiUvJmZRi7/BrYU
-----END CERTIFICATE-----`

	key1Pem = `
-----BEGIN PRIVATE KEY-----
MIIEwAIBADANBgkqhkiG9w0BAQEFAASCBKowggSmAgEAAoIBAQC8TDtfy23OKCRn
kSYrKZwuHG5lOmTZgLwoFR1h3NDTkjR9406CjnAy6Gl73CRG3zRYVgY/2dGNqTzA
KRCeKZlOzBlK6Kilb0NIJ6its6ooMAxwXlr7jOKiSn6xbaexVMrP0VPUbCgJxQtG
s3++hQ14D6WnyfdzPBZJLKbItVdDnAcl/FJXKVV9gIg+MM0gETWOYj5Yd8Ye0FTv
oFcgs8NKkxhEZe/LeYa7XYskS0PymwbHwNZcfC4znp2bzu28LUmUe6kL97YU8ubv
hR0muRy6h5MnQNMQrRG5Q5j4A2+tkO0vto8gOb6/lacEUVYuQdSkMZJiqWEjWgWK
eAYdkTJDAgMBAAECggEBAJTemFqmVQwWxKF1Kn4ZibcTF1zFDBLCKwBtoStMD3YW
M5YL7nhd8OruwOcCJ1Q5CAOHD63PolOjp7otPUwui1y3FJAa3areCo2zfTLHxxG6
2zrD/p6+xjeVOhFBJsGWzjn7v5FEaWs/9ChTpf2U6A8yH8BGd3MN4Hi96qboaDO0
fFz3zOu7sgjkDNZiapZpUuqs7a6MCCr2T3FPwdWUiILZF2t5yWd/l8KabP+3QvvR
tDU6sNv4j8e+dsF2l9ZT81JLkN+f6HvWcLVAADvcBqMcd8lmMSPgxSbytzKanx7o
wtzIiGkNZBCVKGO7IK2ByCluiyHDpGul60Th7HUluDECgYEA9/Q1gT8LTHz1n6vM
2n2umQN9R+xOaEYN304D5DQqptN3S0BCJ4dihD0uqEB5osstRTf4QpP/qb2hMDP4
qWbWyrc7Z5Lyt6HI1ly6VpVnYKb3HDeJ9M+5Se1ttdwyRCzuT4ZBhT5bbqBatsOU
V7+dyrJKbk8r9K4qy29UFozz/38CgYEAwmhzPVak99rVmqTpe0gPERW//n+PdW3P
Ta6ongU8zkkw9LAFwgjGtNpd4nlk0iQigiM4jdJDFl6edrRXv2cisEfJ9+s53AOb
hXui4HAn2rusPK+Dq2InkHYTGjEGDpx94zC/bjYR1GBIsthIh0w2G9ql8yvLatxG
x6oXEsb7Lz0CgYEA7Oj+/mDYUNrMbSVfdBvF6Rl2aHQWbncQ5h3Khg55+i/uuY3K
J66pqKQ0ojoIfk0XEh3qLOLv0qUHD+F4Y5OJAuOT9OBo3J/OH1M2D2hs/+JIFUPT
on+fEE21F6AuvwkXIhCrJb5w6gB47Etuv3CsOXGkwEURQJXw+bODapB+yc0CgYEA
t7zoTay6NdcJ0yLR2MZ+FvOrhekhuSaTqyPMEa15jq32KwzCJGUPCJbp7MY217V3
N+/533A+H8JFmoNP+4KKcnknFb2n7Z0rO7licyUNRdniK2jm1O/r3Mj7vOFgjCaz
hCnqg0tvBn4Jt55aziTlbuXzuiRGGTUfYE4NiJ2vgTECgYEA8di9yqGhETYQkoT3
E70JpEmkCWiHl/h2ClLcDkj0gXKFxmhzmvs8G5On4S8toNiJ6efmz0KlHN1F7Ldi
2iVd9LZnFVP1YwG0mvTJxxc5P5Uy5q/EhCLBAetqoTkWYlPcpkcathmCbCpJG4/x
iOmuuOfQWnMfcVk8I0YDL5+G9Pg=
-----END PRIVATE KEY-----`
)

// TestBuildSecret verifies that BuildSecret returns expected secret.
func TestBuildSecret(t *testing.T) {
	CertPem := []byte(cert1Pem)
	KeyPem := []byte(key1Pem)
	serviceAccount := ""
	namespace := "default"
	secretType := "secret-type"
	caSecret := BuildSecret(serviceAccount, CASecret, namespace,
		nil, nil, nil, CertPem, KeyPem, v1.SecretType(secretType))
	if caSecret.ObjectMeta.Annotations != nil {
		t.Fatalf("Annotation should be nil but got %v", caSecret)
	}
	if caSecret.Data[CertChainID] != nil {
		t.Fatalf("Cert chain should be nil but got %v", caSecret.Data[CertChainID])
	}
	if caSecret.Data[PrivateKeyID] != nil {
		t.Fatalf("Private key should be nil but got %v", caSecret.Data[PrivateKeyID])
	}
	if !bytes.Equal(caSecret.Data[caCertID], CertPem) {
		t.Fatalf("CA cert does not match, want %v got %v", CertPem, caSecret.Data[caCertID])
	}
	if !bytes.Equal(caSecret.Data[caPrivateKeyID], KeyPem) {
		t.Fatalf("CA cert does not match, want %v got %v", KeyPem, caSecret.Data[caPrivateKeyID])
	}

	serverSecret := BuildSecret(serviceAccount, CASecret, namespace,
		CertPem, KeyPem, nil, nil, nil, v1.SecretType(secretType))
	if serverSecret.ObjectMeta.Annotations != nil {
		t.Fatalf("Annotation should be nil but got %v", serverSecret)
	}
	if serverSecret.Data[caCertID] != nil {
		t.Fatalf("CA Cert should be nil but got %v", serverSecret.Data[caCertID])
	}
	if serverSecret.Data[caPrivateKeyID] != nil {
		t.Fatalf("CA private key should be nil but got %v", serverSecret.Data[caPrivateKeyID])
	}
	if !bytes.Equal(serverSecret.Data[CertChainID], CertPem) {
		t.Fatalf("Cert chain does not match, want %v got %v", CertPem, serverSecret.Data[CertChainID])
	}
	if !bytes.Equal(serverSecret.Data[PrivateKeyID], KeyPem) {
		t.Fatalf("Private key does not match, want %v got %v", KeyPem, serverSecret.Data[PrivateKeyID])
	}

	serviceAccount = "account"
	serverSecret = BuildSecret(serviceAccount, CASecret, namespace,
		CertPem, KeyPem, nil, nil, nil, v1.SecretType(secretType))
	if serverSecret.ObjectMeta.Annotations == nil {
		t.Fatal("Annotation should not be nil")
	}
	val, ok := serverSecret.ObjectMeta.Annotations[ServiceAccountNameAnnotationKey]
	if !ok {
		t.Fatalf("Failed to find annotation for %s", ServiceAccountNameAnnotationKey)
	}
	if val != serviceAccount {
		t.Fatalf("annotation does not match, got %s want %s", val, serviceAccount)
	}
}
