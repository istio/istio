package util

import (
	"fmt"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

const (
	// The ID/name for the certificate chain in kubernetes tls secret.
	tlsScrtCert = "tls.crt"
	// The ID/name for the k8sKey in kubernetes tls secret.
	tlsScrtKey = "tls.key"
	// The ID/name for the certificate chain in kubernetes generic secret.
	genericScrtCert = "cert"
	// The ID/name for the private key in kubernetes generic secret.
	genericScrtKey = "key"
	// The ID/name for the CA certificate in kubernetes generic secret.
	genericScrtCaCert = "cacert"
	TlsCertA          = `-----BEGIN CERTIFICATE-----
MIIC8zCCAdugAwIBAgIRAP3c/nKjm5bIlq1JSAiH04swDQYJKoZIhvcNAQELBQAw
GDEWMBQGA1UEChMNY2x1c3Rlci5sb2NhbDAeFw0xOTA1MjAwNDA1MThaFw0yNDA1
MTgwNDA1MThaMBMxETAPBgNVBAoTCEp1anUgb3JnMIIBIjANBgkqhkiG9w0BAQEF
AAOCAQ8AMIIBCgKCAQEAuOntOWs+c5KjjwOlAFtsnhpxDNcnqPUq0+xq13XUvrjf
aqBtvAqfVY3r4HTfO/FLsiTIbnhytMoVVpydUQTW2JdaoKi07RgN89/WoQeu+mZl
F1Co3JjAxlxuDT8dGGJVrXo/KVAU+Yq55UWeUeRHh1Z3RdeyqlP3HcRaaVJ9L0Sw
5WCJjYiooA7ZsbqL0JHEpIzykI7N337nbHzuMooAzyXC+s6HfV1uKrvPqwQWb06u
wSwYcw15XADMGBPiLumU/+4D7lxsAA/x+mBX/kr/M0fNEbNpGh7Gs+1jAHUcvLJ5
TfK4jiVB7kGRCZtS8lbHUNIa0zJBWa6/rSzL7Y6GZQIDAQABoz0wOzAOBgNVHQ8B
Af8EBAMCBaAwDAYDVR0TAQH/BAIwADAbBgNVHREBAf8EETAPgg0qLmV4YW1wbGUu
Y29tMA0GCSqGSIb3DQEBCwUAA4IBAQCdoqXh5FP8DhaSZXe+eBegnmYNgP7NtqLu
085fbqRdg9yLUQGpHzCXnYnyNJB8ML6t3xXTYeakEBvKtGAgqXJ3ekhuTcxgwzlZ
FWuGNFxRjvuVemlPW0ZkIeGEmWm69pJx0s+gIq45+91vS6bsKzw+bAgXFhOWnshU
9yVPZpcbCEBsx4T3c2q/dvbMYQggRJ87cftZW12QRHRDfL/z0WXoPX4AkY7wPfYX
GL1kMpJO8kq5El4EQJg8deHScLuXno6vdjxKw1YfJNEyFJJ0tLbTB0cNLULnxC29
sDAcimMwIw16gI611PSImGfkZ5WPEQueAzSBGeFOxibEPQ2nvh7h
-----END CERTIFICATE-----`
	TlsKeyA = `-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEAuOntOWs+c5KjjwOlAFtsnhpxDNcnqPUq0+xq13XUvrjfaqBt
vAqfVY3r4HTfO/FLsiTIbnhytMoVVpydUQTW2JdaoKi07RgN89/WoQeu+mZlF1Co
3JjAxlxuDT8dGGJVrXo/KVAU+Yq55UWeUeRHh1Z3RdeyqlP3HcRaaVJ9L0Sw5WCJ
jYiooA7ZsbqL0JHEpIzykI7N337nbHzuMooAzyXC+s6HfV1uKrvPqwQWb06uwSwY
cw15XADMGBPiLumU/+4D7lxsAA/x+mBX/kr/M0fNEbNpGh7Gs+1jAHUcvLJ5TfK4
jiVB7kGRCZtS8lbHUNIa0zJBWa6/rSzL7Y6GZQIDAQABAoIBAA5ylIrecrgz9lyH
s94lxoRJ25hdvScJ1MlPGP/xoGgwaKBbnYdKKy3Tk2DrsL9tuHTYQ+LBvXCbs3Qb
c30vjBvLI5UW6V/296wcype1UnoVAwQB1Ne7haBZ+21Eh6DumfQeb43qSGFA8gpU
WKzcUaxk2JfX5QlC2zVzsH63J7nIGXNShIDjl5zZyfYUEXFIPZLOzJtPSfCbvkKE
YgHcrb73SD66kmc/woY5czq6F97kNabgYOcsU8cQZwYGTVDRtYSI5aZsIjVnxadC
U1teyKBEmYxtJ4oLDFe0qsFwPzwSLcQLgFq542vx2E5DZwRZ3aBC2e4r3g2kIZvB
JXEBInECgYEA8/VmeIt2Lv7QH6j6ho3mRKWMAmFgIF6CMIYzEJH6r/Y9VRE2xIe9
/ksih/+6Lq7RWrg2mwDv8FBU6MyIZZOGUza5VWvHzdLfinmGLDyVIScqqbVDvQF1
EyOwNarIwlj/wIQiEGGWBvi8qZ1rRbfjrlerUDH75QCkWMq4DDgSl0kCgYEAwgpz
ac3Y9KVMr0q3mGdOpCpH4rSGSB5wCOdOagBQ8QZAiQJf4qpSbkyjrmZ0S9fIhM0J
zI952pFso8FEYG29rmnE9VZSF8NAGY8zrm8i7pFwg6AYLrlEkSdy36/dQ8AxhIHb
1fuzvo33R8H+cUfndluX0iciZsV4h1ZOl8Faqj0CgYAbbVN/6e33ip5LcOv5hKqG
vTXobpooCXgJjIzhKAhPEBgFIFJP9hLeLARN1epQpUbUNDGva4OOOPnS0mvjP5qy
cEyV1fA4q6SGJPN4tbbua0DYo5BiB2/qHvEIl5LKhsb6FeDehpofXoeXaiNNS0dF
qoWQFo6DSHcxpFjcxtEQQQKBgQCH5yPwjdEPgBrWhyFRp8Fnr4lLmh6WsmLLiZ3d
Fj2aokNe8n/P1HUJdboKcw2u9QInKShc0nyI/eO2Sa2nUBVS7BebsYqrw//IJwkO
eh5gMxM3zVBCoVYJyDRnwNfbFOhZo04icDjzFKGF67RXCQJvXjVWZjxs+I+zUlqX
ZUAoDQKBgCa5QtsIsB3Pb1vLwtiVMzPEOsyhzCilIDwYdNnwYN/FUN/3x5XQa6Lk
5q+J0E+l9i3Z4RSwcv8lvMDO+PQ0wje/sIFJYXDYdyHVjfjCw8VeMaX/lBCgqPPD
37el6OwXaTiA3iMsMI7wxdYNatrTUH+WpfpOX24nu8wiFPWV4uog
-----END RSA PRIVATE KEY-----`
	CaCertA = `-----BEGIN CERTIFICATE-----
MIIC3TCCAcWgAwIBAgIQS/dEZzWQWjY/u9UvprczZzANBgkqhkiG9w0BAQsFADAY
MRYwFAYDVQQKEw1jbHVzdGVyLmxvY2FsMB4XDTE5MDUwODE2NDUwNVoXDTIwMDUw
NzE2NDUwNVowGDEWMBQGA1UEChMNY2x1c3Rlci5sb2NhbDCCASIwDQYJKoZIhvcN
AQEBBQADggEPADCCAQoCggEBAMj+gUO+Q/dBKuoy09AxiOA0aS9mlIRSZjcPaQer
w8tcptp+OHw16427CGIIWNIbbL8ZY48ghKDzoQ7URZ821MafrCsFy+TajBuW8Tuk
s2O+IJ5VL/ZdL5lSD+ytGEplWsuOCaoi23sMBBU6dORjyV1DCPjjcOoYIJAYHBEG
ZQjJJ8ASCifg1nnDiqP7PRb8u2sxcXI2I8W8bv+NMOMkXtt9H5FoCwl1enotj3Sc
nkBe2pqaVhnXEW6MmB2NyOdixVlASfTbE/LetpDjd5Vtl7oSzsYUfVDYV3NzVjNP
fPDcPhPagoyI8G32qWaNoi/AN5ACkokdi0aHNPQyK2w3wYkCAwEAAaMjMCEwDgYD
VR0PAQH/BAQDAgIEMA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcNAQELBQADggEB
AHoMby5yPXzNU4BOZpjElWmYc/M93tvhIA371zANabXVqGWhvHYZLGWsmoA9zZZj
jBwVh1IznR2hEUfKvFKuRTytA5YyehmzI3Ro0drTNlRTcVD7+cHrOKv8wiwwXG8H
68RW0O/h6Ew6FDa67RIXoQbIdyCkRQJH0KkOEkgOWeO6OKRMLg5av7QQ+U7BmME6
GKrZhCuiLpeR0mQ6TSW9GGIGsFizDccNmqXBtAWo2fthQ+Z/7hLj7GsQmUpk+6n+
yQ2mq7pkOghaxpjrPFK1BJUTehTE/d7uz6C3bisQCtRUEI6vtk/WF4ZT6djSaMpx
IQ0V5r5pTBIkjWHRrkrYTEs=
-----END CERTIFICATE-----`
	TlsCertB = `-----BEGIN CERTIFICATE-----
MIIDFjCCAf6gAwIBAgIRAN/GwM061YoVYQjJskaHqp4wDQYJKoZIhvcNAQELBQAw
GDEWMBQGA1UEChMNY2x1c3Rlci5sb2NhbDAeFw0xOTA0MjEwMTQzMzFaFw0yOTA0
MTgwMTQzMzFaMBMxETAPBgNVBAoTCEp1anUgb3JnMIIBIjANBgkqhkiG9w0BAQEF
AAOCAQ8AMIIBCgKCAQEA8Vcne4hySZSiYdehjQlqLPu63WlbzZl48C9XzC3lMj/c
5efSFgPq9byMi9GvSSRn1fx6GoeqT9uK1LsNf0NCwOkmAbzv1tjGlVXU1PZxj8Rt
Ox5gymdc/YaeGJUt1FA7ndWlsKxROMdKwXfZXbSqTnr+1ohmuH/rM6W9GHZjlxw4
4+erWo0Pzm9/Q8yhM8qAh1VMqDbSG/TnsNKroIz4NN4cCCl3EFnMISdS4NJijbTE
4Mdb3XZ2qvm2YZVQFkvoenDeZtM3TjFwl64Y4/myrbom/BYwRCEmk89cayv29pCv
/0luA+dlN+fjYDKGwFzG9aV7fbbn4INbD1E8v88l3QIDAQABo2AwXjAOBgNVHQ8B
Af8EBAMCBaAwDAYDVR0TAQH/BAIwADA+BgNVHREENzA1hjNzcGlmZmU6Ly9mb28t
ZG9tYWluL25zL2Jhci1uYW1lc3BhY2Uvc2EvYmF6LWFjY291bnQwDQYJKoZIhvcN
AQELBQADggEBACeCXuGb7z67l7927G3ser1XYW5p72P1ixb2Fpsh6Y4Ry1lhRtIQ
pBYg41U+a7fMwtVkfKP+M+U0b/8pPfdvOy877mqAKByAmbF5B6/i3Dj83fdVBVyM
enSHeUaeJEe7goGhPkuYvTg190eE898yae6PIdN9PQ7nVOW9bPA79FmndZLg/0a+
YUCBXSQSlhMpVIAuYorOTlhpkcg5e80TfTNZ5r1y4MOH8UkTQaNbu9EGRbfhRx/p
N5mU6UOLYrH4xJWwhbor0cSDiKwr11H5i7G1dreMX/oWURg61zEj+frTRmD4cXiz
ks34RLGuoryBBoYftQ2Z/Jn5HjCnzkm/qtQ=
-----END CERTIFICATE-----`
	TlsKeyB = `-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEA8Vcne4hySZSiYdehjQlqLPu63WlbzZl48C9XzC3lMj/c5efS
FgPq9byMi9GvSSRn1fx6GoeqT9uK1LsNf0NCwOkmAbzv1tjGlVXU1PZxj8RtOx5g
ymdc/YaeGJUt1FA7ndWlsKxROMdKwXfZXbSqTnr+1ohmuH/rM6W9GHZjlxw44+er
Wo0Pzm9/Q8yhM8qAh1VMqDbSG/TnsNKroIz4NN4cCCl3EFnMISdS4NJijbTE4Mdb
3XZ2qvm2YZVQFkvoenDeZtM3TjFwl64Y4/myrbom/BYwRCEmk89cayv29pCv/0lu
A+dlN+fjYDKGwFzG9aV7fbbn4INbD1E8v88l3QIDAQABAoIBAAID0266xRLWh1k1
cYIEcJr/NQsGh1Ta8awLsSTMGKGdRTe0LPMFxa9B4OiFRSc2rcPsb4vg0xax7gxN
otS2V+HVcH2g5AhmrJhwQYPPqkNkL/HyPyZMIPYgQBn+G4ZYxHlSYHzuS1/5JfzM
hoeQ67/A/iIGEdl7fpgNS7FfF6tvdZFsSSiYzDt1XNYtgAypzUYaupgCCqbfGe8/
zRXFQc2XM5ptJqD4iIklft4xjTgs5ANuIbrnHqt9fZK9/bpnbE1jJJN3G2kiLGkG
DEzauG8WJdfzWnzhaBUoi6vBlB+Y8AMZx4Ixllr8d2HcPBOfMZRJiK0LxsD4TM1Z
ISGpsu8CgYEA/RxVIMoiIHGz9h0gAQZgo73zWa/f7xxVt9nrzHaJYIV5V0kafbmV
ODCly0WEUp1hMHKKdPMGDCyPe4N2Wm7lX6plUFKIEGwVAeiOiAr4KL2JWcRDlPJo
eUFzbjifGvn+u88z4qw/EDUEnri/hKUnkqzHWA5M8QL/KDqfTEos3ysCgYEA9Bhs
6v/KObX47AHx/0+3Fux4oDQBvRBsq5TmjzQlolyAXB5iTlr7FMCJAkEzW1CTvrNC
DS/A2s7QCfHlqPkgzdVfjct0dcMJYqlLZMISK0kz4B7bDp3MgCRyx50HVxvPgTst
MsXLehgaYb4p+QzPvUzKqUj+SwsvnyZyjZpwyxcCgYAhDEr9LgdIry/tKZ5dI+UI
XCvjAPi/Mrbqe3SzTKLhTGwsfmoMEmguXwO2x8vgMZZYCgyT+otGmabeXKreYe5n
EEuMMkp7wnD3v9KkZrJCN4UwiFS+pOwJMQeOU6xKjGu7P/GpXg4Z4qJIyxyOiDXj
i9W3ZJ6dNWP1b7oO7vxu4wKBgQCZv8jbPMLkFvrzrUYAyvVIOyq/vgJaVD4e1Wtk
SDRsUFeJrpm9QRFlwOCLywXOPrLRK5gvNiUDrcDcgsFl7YX8IKpPZhe1FWSUAI68
qIFJQpKqWMUiL8Lf9BVYJlC5TYsmm1+c23mPLh9v8Zf+h1NSqUv91TxXiHQ2isEc
8GqbgQKBgH29F9JIoHGnseOiaHtl6wJ1HgiFYMlpDkm+vpoY6ANBh3vn+3Toar06
7ymVRYkJd+VMzDLvxcGazM0L9W1toKCQZtvSqFC0IWoCkOxbWXOGGGYvnQlnjOTq
QeT4wjUtrpqi0UtSBmcPzlfavCU6LdMlTfQaJTSKBhphI1fYX3bX
-----END RSA PRIVATE KEY-----`
	CaCertB = `-----BEGIN CERTIFICATE-----
MIIC3jCCAcagAwIBAgIRAKy6gtfNLI5YGd10eSc+C90wDQYJKoZIhvcNAQELBQAw
GDEWMBQGA1UEChMNY2x1c3Rlci5sb2NhbDAeFw0xOTA0MTUxOTA3MjNaFw0yOTA0
MTIxOTA3MjNaMBgxFjAUBgNVBAoTDWNsdXN0ZXIubG9jYWwwggEiMA0GCSqGSIb3
DQEBAQUAA4IBDwAwggEKAoIBAQCz6+Y52zPwDQNshYdWkMr7eqCPNTfNyPWqAHH0
G9Q7/Lt0O29xOBFsAiMzrGYzboXc7Nc4EJs+hpIrEuz+cUpqc/7THsyiEOHz0n+S
bG97inkaVUccAzO39hwQh0ULsbOYOJBqw89Hi3DKPgIBH61Oa+6Z/+U9XjXoQ8ah
Bd36efD7o1crgDh7AnQCZXo7JkOeEFwo39i0RIk1PmbuSMJZZbI2QJFxza+5voe3
7iZ8Q2fk1SxW/71u4Q5kZfIIbzJcMr77N9J/yVHvh6zD7ovYZXZvO0UUzka1x2p+
kYrFhTiQJHVBLaabjDTr91C76G0HZyQ9L4Op62D7Kq2fqXJ5AgMBAAGjIzAhMA4G
A1UdDwEB/wQEAwICBDAPBgNVHRMBAf8EBTADAQH/MA0GCSqGSIb3DQEBCwUAA4IB
AQCvFc803CKFGuEObGrWBBTQiqRcbMnoiVPieRubvMLQwUQ+q6Vln8f1nuJYIm0u
pPVw4A7qKzA1cnqswPAhTiVDgWVLZHzAa9U6nmhPqgyMszdoHheMX8JZrgBpkviy
ogn79E51ywafTuQ5KHzqIdpoUHlxNE99owDQ3Lez2tVWLJjO9sdhDYUJLYgpCnvr
FatenWY9f9d5Na+KLgWxSkg54mBhAF+HAT4feap1lSdumX4Y6oouufpbfz6B6nEh
iymngayzaKLHK+n4ZVtk/VkGhY7OzvcR6MAXu0uBwEgwLo0AXW0Um5bGcpPdfRqn
oke6Hv5bkEFTm2GJ7GXS/6Z9
-----END CERTIFICATE-----`
)

type ingressCredential struct {
	PrivateKey string
	ServerCert string
	CaCert     string
}

var IngressCredentialA = ingressCredential{
	PrivateKey: TlsKeyA,
	ServerCert: TlsCertA,
	CaCert:     CaCertA,
}
var IngressCredentialB = ingressCredential{
	PrivateKey: TlsKeyB,
	ServerCert: TlsCertB,
	CaCert:     CaCertB,
}

// CreateIngressKubeSecret reads credential names from credNames and create K8s secrets for
// ingress gateway.
func CreateIngressKubeSecret(t *testing.T, ctx framework.TestContext, credNames []string,
	ingressType ingress.IgType) {
	// Get namespace for ingress gateway pod.
	istioCfg := istio.DefaultConfigOrFail(t, ctx)
	systemNS := namespace.ClaimOrFail(t, ctx, istioCfg.SystemNamespace)

	if len(credNames) == 0 {
		t.Log("no credential names are specified, skip creating ingress secret")
		return
	}
	// Create Kubernetes secret for ingress gateway
	kubeAccessor := ctx.Environment().(*kube.Environment).Accessor
	for _, cn := range credNames {
		secret := createSecret(ingressType, cn, systemNS.Name(), &IngressCredentialA)
		err := kubeAccessor.CreateSecret(systemNS.Name(), secret)
		if err != nil {
			t.Errorf("Failed to create secret (error: %s)", err)
		}
	}
	// Check if Kubernetes secret is ready
	maxRetryNumber := 5
	checkRetryInterval := time.Second * 1
	for _, cn := range credNames {
		t.Logf("Check ingress Kubernetes secret %s:%s...", systemNS.Name(), cn)
		for i := 0; i < maxRetryNumber; i++ {
			_, err := kubeAccessor.GetSecret(systemNS.Name()).Get(cn, metav1.GetOptions{})
			if err != nil {
				time.Sleep(checkRetryInterval)
			} else {
				t.Logf("Secret %s:%s is ready.", systemNS.Name(), cn)
				break
			}
		}
	}
}

// createSecret creates a kubernetes secret which stores private key, server certificate for TLS ingress gateway.
// For mTLS ingress gateway, createSecret adds ca certificate into the secret object.
func createSecret(ingressType ingress.IgType, cn, ns string, ic *ingressCredential) *v1.Secret {
	if ingressType == ingress.Mtls {
		return &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cn,
				Namespace: ns,
			},
			Data: map[string][]byte{
				genericScrtCert:   []byte(ic.ServerCert),
				genericScrtKey:    []byte(ic.PrivateKey),
				genericScrtCaCert: []byte(ic.CaCert),
			},
		}
	}
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cn,
			Namespace: ns,
		},
		Data: map[string][]byte{
			tlsScrtCert: []byte(ic.ServerCert),
			tlsScrtKey:  []byte(ic.PrivateKey),
		},
	}
}

// VisitProductPage makes HTTPS request to ingress gateway to visit product page
func VisitProductPage(ingress ingress.Instance, host string, timeout time.Duration, wantStatus int, t *testing.T) error {
	start := time.Now()
	for {
		response, err := ingress.Call("/productpage", host)
		if err != nil {
			t.Logf("Unable to connect to product page: %v", err)
		}

		status := response.Code
		if status == wantStatus {
			t.Logf("Got %d response from product page!", wantStatus)
			return nil
		} else {
			t.Errorf("expected response code %d but got %d", wantStatus, status)
		}

		if time.Since(start) > timeout {
			return fmt.Errorf("could not retrieve product page in %v: Last status: %v", timeout, status)
		}

		time.Sleep(3 * time.Second)
	}
}

func RotateSecret(secretName string) {

}
