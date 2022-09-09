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
	"google.golang.org/protobuf/types/known/durationpb"
	"k8s.io/apimachinery/pkg/util/rand"

	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/util/sets"
	"istio.io/pkg/log"
)

var googleCASClientLog = log.RegisterScope("googlecas", "Google CAS client debugging", 0)

// GoogleCASClient: Agent side plugin for Google CAS
type GoogleCASClient struct {
	caSigner string
	caClient *privateca.CertificateAuthorityClient
}

// NewGoogleCASClient create a CA client for Google CAS.
func NewGoogleCASClient(capool string, options ...option.ClientOption) (security.Client, error) {
	caClient := &GoogleCASClient{caSigner: capool}
	ctx := context.Background()
	var err error

	caClient.caClient, err = privateca.NewCertificateAuthorityClient(ctx, options...)

	if err != nil {
		googleCASClientLog.Errorf("unable to initialize google cas caclient: %v", err)
		return nil, err
	}
	googleCASClientLog.Debugf("Intitialized Google CAS plugin with endpoint: %v", capool)
	return caClient, nil
}

func (r *GoogleCASClient) createCertReq(name string, csrPEM []byte, lifetime time.Duration) *privatecapb.CreateCertificateRequest {
	isCA := false

	// We use Certificate_Config option to ensure that we only request a certificate with CAS supported extensions/usages.
	// CAS uses the PEM encoded CSR only for its public key and infers the certificate SAN (identity) of the workload through SPIFFE identity reflection
	creq := &privatecapb.CreateCertificateRequest{
		Parent:        r.caSigner,
		CertificateId: name,
		Certificate: &privatecapb.Certificate{
			Lifetime: durationpb.New(lifetime),
			CertificateConfig: &privatecapb.Certificate_Config{
				Config: &privatecapb.CertificateConfig{
					SubjectConfig: &privatecapb.CertificateConfig_SubjectConfig{
						Subject: &privatecapb.Subject{},
					},
					X509Config: &privatecapb.X509Parameters{
						KeyUsage: &privatecapb.KeyUsage{
							BaseKeyUsage: &privatecapb.KeyUsage_KeyUsageOptions{
								DigitalSignature: true,
								KeyEncipherment:  true,
							},
							ExtendedKeyUsage: &privatecapb.KeyUsage_ExtendedKeyUsageOptions{
								ServerAuth: true,
								ClientAuth: true,
							},
						},
						CaOptions: &privatecapb.X509Parameters_CaOptions{
							IsCa: &isCA,
						},
					},
					PublicKey: &privatecapb.PublicKey{
						Format: privatecapb.PublicKey_PEM,
						Key:    csrPEM,
					},
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

	rand.Seed(time.Now().UnixNano())
	name := fmt.Sprintf("csr-workload-%s", rand.String(8))
	creq := r.createCertReq(name, csrPEM, time.Duration(certValidTTLInSec)*time.Second)

	ctx := context.Background()

	cresp, err := r.caClient.CreateCertificate(ctx, creq)
	if err != nil {
		googleCASClientLog.Errorf("unable to create certificate: %v", err)
		return []string{}, err
	}
	certChain = append(certChain, cresp.GetPemCertificate())
	certChain = append(certChain, cresp.GetPemCertificateChain()...)
	return certChain, nil
}

// GetRootCertBundle:  Get CA certs of the pool from Google CAS API endpoint
func (r *GoogleCASClient) GetRootCertBundle() ([]string, error) {
	rootCertSet := sets.New()

	ctx := context.Background()

	req := &privatecapb.FetchCaCertsRequest{
		CaPool: r.caSigner,
	}
	resp, err := r.caClient.FetchCaCerts(ctx, req)
	if err != nil {
		googleCASClientLog.Errorf("error when getting root-certs from CAS pool: %v", err)
		return nil, err
	}
	for _, certChain := range resp.CaCerts {
		certs := certChain.Certificates
		rootCert := certs[len(certs)-1]
		rootCertSet.Insert(rootCert)
	}

	return rootCertSet.SortedList(), nil
}

func (r *GoogleCASClient) Close() {
	_ = r.caClient.Close()
}
