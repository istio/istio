package util

import (
	"fmt"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
	"time"
)

const (
	// The ID/name for the certificate chain in kubernetes tls secret.
	tlsScrtCert = "tls.crt"
	// The ID/name for the k8sKey in kubernetes tls secret.
	tlsScrtKey = "tls.key"
	tlsCert = `-----BEGIN CERTIFICATE-----
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
	tlsKey = `-----BEGIN RSA PRIVATE KEY-----
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
	caCert = `-----BEGIN CERTIFICATE-----
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
)

// CreateIngressKubeSecret reads credential names from credNames and create K8s secrets for
// ingress gateway.
func CreateIngressKubeSecret(t *testing.T, ctx framework.TestContext, credNames []string) {
	// Get namespace for ingress gateway pod.
	istioCfg := istio.DefaultConfigOrFail(t, ctx)
	systemNS := namespace.ClaimOrFail(t, ctx, istioCfg.SystemNamespace)

	if len(credNames) == 0 {
		t.Log("no credential names are specified, skip creating ingress secret")
		return
	}
	for _, cn := range credNames {
		secret := v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cn,
				Namespace: systemNS.Name(),
			},
			Data: map[string][]byte{
				tlsScrtCert: []byte(tlsCert),
				tlsScrtKey:  []byte(tlsKey),
			},
		}
		err := ctx.Environment().(*kube.Environment).Accessor.CreateSecret(systemNS.Name(), &secret)
		if err != nil {
			t.Errorf("Failed to create secret (error: %s)", err)
		}
	}
}

func VisitProductPage(ingress ingress.Instance, timeout time.Duration, wantStatus int, t *testing.T) error {
	start := time.Now()
	for {
		response, err := ingress.Call("/productpage")
		if err != nil {
			t.Logf("Unable to connect to product page: %v", err)
		}

		status := response.Code
		if status == wantStatus {
			t.Logf("Got %d response from product page!", wantStatus)
			return nil
		}

		if time.Since(start) > timeout {
			return fmt.Errorf("could not retrieve product page in %v: Last status: %v", timeout, status)
		}

		time.Sleep(3 * time.Second)
	}
}