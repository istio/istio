package ca

import (
	"context"
	"fmt"
	"testing"

	pb "istio.io/api/security/v1alpha1"
	"istio.io/istio/pkg/fuzz"
	"istio.io/istio/pkg/security"
	mockca "istio.io/istio/security/pkg/pki/ca/mock"
	caerror "istio.io/istio/security/pkg/pki/error"
)

func FuzzCreateCertificate(f *testing.F) {
	fuzz.BaseCases(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		fg := fuzz.New(t, data)
		csr := fuzz.Struct[pb.IstioCertificateRequest](fg)
		ca := fuzz.Struct[mockca.FakeCA](fg)
		ca.SignErr = caerror.NewError(caerror.CSRError, fmt.Errorf("cannot sign"))
		server := &Server{
			ca:             &ca,
			Authenticators: []security.Authenticator{&mockAuthenticator{}},
			monitoring:     newMonitoringMetrics(),
		}
		_, _ = server.CreateCertificate(context.Background(), &csr)
	})
}
