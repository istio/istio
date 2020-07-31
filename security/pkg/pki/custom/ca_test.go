package custom

import (
	"context"
	"testing"

	pb "istio.io/api/security/v1alpha1"
	mock "istio.io/istio/security/pkg/pki/custom/mock"
	certutil "istio.io/istio/security/pkg/pki/util"
)

func TestRequestCertificate(t *testing.T) {

	fakeServer := mock.NewFakeExternalCA()
	addr, err := fakeServer.Serve()

	if err != nil {
		t.Errorf("cannot create fake server: %v", err)
	}

	opts := certutil.CertOptions{
		Host:       "spiffe://cluster.local/sa/test-service-service-account",
		RSAKeySize: 2048,
		IsCA:       false,
	}

	csrPEM, _, err := certutil.GenCSR(opts)

	keyCertBundle, _ := certutil.NewKeyCertBundleWithRootCertFromFile("../testdata/multilevelpki/root-cert.pem")

	c, err := NewCAClient(&CAClientOpts{
		CAAddr:        addr.String(),
		KeyCertBundle: keyCertBundle,
	})

	if err != nil {
		t.Errorf("cannot create custom CA client: %v", err)
	}

	res, err := c.CreateCertificate(context.TODO(), &pb.IstioCertificateRequest{Csr: string(csrPEM)})
	if err != nil {
		t.Errorf("expect not failed but got error: %v", err)
	}

	if len(res.GetCertChain()) != 2 {
		t.Errorf("expect len of CertChain is 2 but got: %v", len(res.GetCertChain()))
	}

}
