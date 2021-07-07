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

package caclient

import (
	"context"
	"fmt"
	"time"

	privateca "cloud.google.com/go/security/privateca/apiv1"
	"google.golang.org/api/option"
	privatecapb "google.golang.org/genproto/googleapis/cloud/security/privateca/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/durationpb"
	"k8s.io/apimachinery/pkg/util/rand"

	"istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/nodeagent/caclient"
	"istio.io/istio/security/pkg/nodeagent/caclient/providers/google-cas/mock"
	"istio.io/pkg/log"
)

var googleCAClientLog = log.RegisterScope("googlecas", "Google CAS client debugging", 0)

type GoogleCASClient struct {
	caSigner string
	caClient *privateca.CertificateAuthorityClient
	provider *caclient.TokenProvider
}

// NewGoogleCASClient create a CA client for Google CAS.
func NewGoogleCASClient(capool string, provider *caclient.TokenProvider, lis *bufconn.Listener) (security.Client, error) {
	var err error
	caClient := &GoogleCASClient{caSigner: capool, provider: provider}
	ctx := context.Background()

	if lis == nil {
		caClient.caClient, err = privateca.NewCertificateAuthorityClient(ctx,
			option.WithGRPCDialOption(grpc.WithPerRPCCredentials(provider)))
	} else {
		caClient.caClient, err = privateca.NewCertificateAuthorityClient(ctx,
			option.WithoutAuthentication(),
			option.WithGRPCDialOption(grpc.WithContextDialer(mock.ContextDialerCreate(lis))),
			option.WithGRPCDialOption(grpc.WithInsecure()))
	}
	if err != nil {
		googleCAClientLog.Errorf("unable to initialize google cas caclient: %v", err)
		return nil, err
	}
	return caClient, nil
}

func (r *GoogleCASClient) createCertReq(name string, csrPEM []byte, lifetime time.Duration) *privatecapb.CreateCertificateRequest {
	subjectConfig := &privatecapb.CertificateConfig_SubjectConfig{
		// Empty Subject
		Subject: &privatecapb.Subject{},
	}
	keyUsage := &privatecapb.KeyUsage{
		BaseKeyUsage: &privatecapb.KeyUsage_KeyUsageOptions{
			DigitalSignature: true,
			KeyEncipherment:  true,
		},
		ExtendedKeyUsage: &privatecapb.KeyUsage_ExtendedKeyUsageOptions{
			ServerAuth: true,
			ClientAuth: true,
		},
	}

	// Derive Public Key from the csrPEM
	publicKey := &privatecapb.PublicKey{
		// TODO: Need to get key-type from security options
		Format: privatecapb.PublicKey_PEM,
		Key:    csrPEM,
	}

	creq := &privatecapb.CreateCertificateRequest{
		Parent:        r.caSigner,
		CertificateId: name,
		Certificate: &privatecapb.Certificate{
			Lifetime: durationpb.New(lifetime),
			CertificateConfig: &privatecapb.Certificate_Config{
				Config: &privatecapb.CertificateConfig{
					SubjectConfig: subjectConfig,
					X509Config: &privatecapb.X509Parameters{
						KeyUsage: keyUsage,
					},
					PublicKey: publicKey,
				},
			},
			SubjectMode: privatecapb.SubjectRequestMode_REFLECTED_SPIFFE,
		},
	}
	return creq
}

// CSR Sign calls Google CAS to sign a CSR.
func (r *GoogleCASClient) CSRSign(csrPEM []byte, certValidTTLInSec int64) ([]string, error) {
	certChain := []string{}
	name := fmt.Sprintf("csr-workload-%s", rand.String(8))
	creq := r.createCertReq(name, csrPEM, time.Duration(certValidTTLInSec)*time.Second)

	ctx := context.Background()

	cresp, err := r.caClient.CreateCertificate(ctx, creq)
	if err != nil {
		googleCAClientLog.Errorf("unable to create certificate: %v", err)
		return []string{}, err
	}
	certChain = append(certChain, cresp.GetPemCertificate())
	certChain = append(certChain, cresp.GetPemCertificateChain()...)
	return certChain, nil
}

func (r *GoogleCASClient) GetRootCertBundle() ([]string, error) {
	var rootCertMap map[string]struct{} = make(map[string]struct{})
	var trustbundle []string = []string{}
	var err error

	ctx := context.Background()

	req := &privatecapb.FetchCaCertsRequest{
		CaPool: r.caSigner,
	}
	resp, err := r.caClient.FetchCaCerts(ctx, req)
	if err != nil {
		googleCAClientLog.Errorf("error when getting root-certs from CAS pool: %v", err)
		return trustbundle, err
	}
	for _, certChain := range resp.CaCerts {
		certs := certChain.Certificates
		rootCert := certs[len(certs)-1]
		if _, ok := rootCertMap[rootCert]; !ok {
			rootCertMap[rootCert] = struct{}{}
		}
	}

	for rootCert := range rootCertMap {
		trustbundle = append(trustbundle, rootCert)
	}
	return trustbundle, nil
}

func (r *GoogleCASClient) Close() {
}
