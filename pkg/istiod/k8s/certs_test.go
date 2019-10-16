package k8s

import (
	"testing"
	"time"
)

// Generate a certificate. Typical time ~500ms
func TestCerts(t *testing.T) {

	t0 := time.Now()
	client, kcfg, err := CreateClientset("", "")
	if err != nil {
		t.Fatal("Missing K8S", err)
	}

	certChain, keyPEM, err := GenKeyCertK8sCA(client.CertificatesV1beta1(), "istio-system", "istio-pilot.istio-system")
	if err != nil {
		t.Fatal("Fail to generate cert", err)
	}

	caCert := kcfg.TLSClientConfig.CAData
	//certChain = append(certChain, caCert...)

	t.Log("Cert Chain:", time.Since(t0), "\n", string(certChain))
	t.Log("Key\n", string(keyPEM))

	err = CheckCert(certChain, caCert)
	if err != nil {
		t.Fatal("Invalid root CA or cert", err)
	}
}

func BenchmarkCerts(t *testing.B) {

	client, kcfg, err := CreateClientset("", "")
	if err != nil {
		t.Fatal("Missing K8S", err)
	}

	certChain, _, err := GenKeyCertK8sCA(client.CertificatesV1beta1(), "istio-system", "istio-pilot.istio-system")
	if err != nil {
		t.Fatal("Fail to generate cert", err)
	}

	caCert := kcfg.TLSClientConfig.CAData
	certChain = append(certChain, caCert...)
}
