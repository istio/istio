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
// limitations under the License

package ra

import (
	"context"
	"fmt"
	"strings"
	"time"

	privateca "cloud.google.com/go/security/privateca/apiv1beta1"
	privatecapb "google.golang.org/genproto/googleapis/cloud/security/privateca/v1beta1"
	"google.golang.org/protobuf/types/known/durationpb"
	"k8s.io/apimachinery/pkg/util/rand"

	"istio.io/istio/security/pkg/pki/ca"
	raerror "istio.io/istio/security/pkg/pki/error"
	"istio.io/istio/security/pkg/pki/util"
	"istio.io/pkg/log"
)

var casRaLog = log.RegisterScope("casRa", "PrivateCA RA log", 0)

// GoogleCasRA is a Registration Authority that integrates with Google Certificate Authority Service
type GoogleCasRA struct {
	keyCertBundle *util.KeyCertBundle
	raOpts        *IstioRAOptions
	pcaClient     *privateca.CertificateAuthorityClient
}

// NewGoogleCasRA : Create a RA that interfaces with Google Certificate Authority Service
func NewGoogleCasRA(raOpts *IstioRAOptions) (*GoogleCasRA, error) {
	var err error
	istioRA := &GoogleCasRA{raOpts: raOpts}

	ctx := context.Background()
	istioRA.pcaClient, err = privateca.NewCertificateAuthorityClient(ctx)
	if err != nil {
		return nil, raerror.NewError(raerror.CAInitFail, fmt.Errorf("could not initialize certificate authority client %s", err.Error()))
	}

	CA, err := istioRA.pcaClient.GetCertificateAuthority(ctx, &privatecapb.GetCertificateAuthorityRequest{Name: raOpts.CaSigner})
	if err != nil {
		return nil, raerror.NewError(raerror.CAInitFail, fmt.Errorf("could not initialize certificate authority client. Error %s", err.Error()))
	} else if len(CA.PemCaCertificates) > 2 {
		return nil, raerror.NewError(raerror.CAInitFail, fmt.Errorf("could not initialize certificate authority client. Invalidate Cert Chain length"))
	}

	if len(CA.PemCaCertificates) == 2 {
		istioRA.keyCertBundle, err = util.NewUnverifiedKeyCertBundle([]byte{},
			[]byte{}, []byte(CA.PemCaCertificates[0]), []byte(CA.PemCaCertificates[1]))
	} else {
		istioRA.keyCertBundle, err = util.NewUnverifiedKeyCertBundle([]byte{},
			[]byte{}, []byte{}, []byte(CA.PemCaCertificates[0]))
	}
	if err != nil {
		return nil, raerror.NewError(raerror.CAInitFail, fmt.Errorf("could not initialize certificate authority bundle %s", err.Error()))
	}

	casRaLog.Infof("successfully initialized Google Certificate Authority Service Registration Authority")
	return istioRA, nil
}

func (r *GoogleCasRA) createCertReq(name string, csrPEM []byte,
	subjectIDs []string, lifetime time.Duration) *privatecapb.CreateCertificateRequest {
	subjectConfig := &privatecapb.CertificateConfig_SubjectConfig{
		// Empty Subject
		Subject: &privatecapb.Subject{},
		// Only URI Names
		SubjectAltName: &privatecapb.SubjectAltNames{
			Uris: subjectIDs,
		},
	}
	reusableConfigValues := &privatecapb.ReusableConfigValues{
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
	}
	// Derive Public Key from the csrPEM
	publicKey := &privatecapb.PublicKey{
		Type: privatecapb.PublicKey_PEM_RSA_KEY,
		Key:  csrPEM,
	}

	creq := &privatecapb.CreateCertificateRequest{
		Parent:        r.raOpts.CaSigner,
		CertificateId: name,
		Certificate: &privatecapb.Certificate{
			Lifetime: durationpb.New(lifetime),
			CertificateConfig: &privatecapb.Certificate_Config{
				Config: &privatecapb.CertificateConfig{
					SubjectConfig: subjectConfig,
					ReusableConfig: &privatecapb.ReusableConfigWrapper{
						ConfigValues: &privatecapb.ReusableConfigWrapper_ReusableConfigValues{
							ReusableConfigValues: reusableConfigValues,
						},
					},
					PublicKey: publicKey,
				},
			},
		},
	}
	return creq
}

func (r *GoogleCasRA) Sign(csrPEM []byte, certOpts ca.CertOpts) ([]byte, error) {
	lifetime, err := preSign(r.raOpts, csrPEM, certOpts.SubjectIDs, certOpts.TTL, certOpts.ForCA)
	if err != nil {
		return []byte{}, err
	}
	name := fmt.Sprintf("csr-workload-%s", rand.String(8))
	creq := r.createCertReq(name, csrPEM, certOpts.SubjectIDs, lifetime)

	// TODO - Derive context from parent function
	ctx := context.Background()

	cresp, err := r.pcaClient.CreateCertificate(ctx, creq)
	if err != nil {
		return []byte{}, raerror.NewError(raerror.CertGenError, fmt.Errorf("certificate creation on CAS failed because %s", err.Error()))
	}
	casRaLog.Infof("successfully created certificate using google Certificate Authority Service for identity %s!",
		strings.Join(certOpts.SubjectIDs, ","))
	return []byte(cresp.PemCertificate), nil
}

// SignWithCertChain is similar to Sign but returns the leaf cert and the entire cert chain.
func (r *GoogleCasRA) SignWithCertChain(csrPEM []byte, certOpts ca.CertOpts) ([]byte, error) {
	/* TODO Fix: All these implementations that rely on the certBundle functions are incorrect because
	they don't include the root-cert */
	cert, err := r.Sign(csrPEM, certOpts)
	if err != nil {
		return nil, err
	}
	chainPem := r.GetCAKeyCertBundle().GetCertChainPem()
	if len(chainPem) > 0 {
		cert = append(cert, chainPem...)
	}
	return cert, nil
}

// GetCAKeyCertBundle returns the KeyCertBundle for the CA.
func (r *GoogleCasRA) GetCAKeyCertBundle() *util.KeyCertBundle {
	return r.keyCertBundle
}
