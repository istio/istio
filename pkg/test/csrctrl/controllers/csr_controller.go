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

// An example implementation of a CSR Controller.
package csrctrl

import (
	"fmt"
	"strings"
	"time"

	certv1 "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/test/csrctrl/signer"
	"istio.io/istio/security/pkg/pki/util"
)

const RequestLifeTimeAnnotationForCertManager = "experimental.cert-manager.io/request-duration"

type Signer struct {
	csrs    kclient.Client[*certv1.CertificateSigningRequest]
	signers map[string]*signer.Signer
	queue   controllers.Queue
}

func NewSigner(cl kube.Client, signers map[string]*signer.Signer) *Signer {
	c := &Signer{
		csrs:    kclient.New[*certv1.CertificateSigningRequest](cl),
		signers: signers,
	}
	c.queue = controllers.NewQueue("csr",
		controllers.WithReconciler(c.Reconcile),
		controllers.WithMaxAttempts(5))
	c.csrs.AddEventHandler(controllers.ObjectHandler(c.queue.AddObject))
	return c
}

func (s *Signer) Reconcile(key types.NamespacedName) error {
	csr := s.csrs.Get(key.Name, key.Namespace)
	if csr == nil {
		// CSR was deleted, no action needed
		return nil
	}

	var exist bool
	signer, exist := s.signers[csr.Spec.SignerName]
	switch {
	case !csr.DeletionTimestamp.IsZero():
		log.Info("CSR has been deleted. Ignoring.")
	case csr.Spec.SignerName == "":
		log.Info("CSR does not have a signer name. Ignoring.")
	case !exist:
		log.Infof("CSR signer name does not match. Ignoring. signer-name: %s, have %v", csr.Spec.SignerName, strings.Join(maps.Keys(s.signers), ","))
	case csr.Status.Certificate != nil:
		log.Info("CSR has already been signed. Ignoring.")
	case !isCertificateRequestApproved(csr):
		log.Info("CSR is not approved, Ignoring.")
	default:
		log.Info("Signing")
		x509cr, err := util.ParsePemEncodedCSR(csr.Spec.Request)
		if err != nil {
			log.Infof("unable to parse csr: %v", err)
			return nil
		}

		requestedLifeTime := signer.CertTTL
		requestedDuration, ok := csr.Annotations[RequestLifeTimeAnnotationForCertManager]
		if ok {
			duration, err := time.ParseDuration(requestedDuration)
			if err == nil {
				requestedLifeTime = duration
			}
		}
		cert, err := signer.Sign(x509cr, csr.Spec.Usages, requestedLifeTime, false)
		if err != nil {
			return fmt.Errorf("signing csr: %v", err)
		}
		csr.Status.Certificate = cert
		if _, err := s.csrs.UpdateStatus(csr); err != nil {
			return fmt.Errorf("patch: %v", err)
		}
		log.Infof("CSR %q has been signed", csr.Spec.SignerName)
	}
	return nil
}

func (s *Signer) Run(stop <-chan struct{}) {
	kube.WaitForCacheSync("csr", stop, s.csrs.HasSynced)
	s.queue.Run(stop)
}

func (s *Signer) HasSynced() bool {
	return s.queue.HasSynced()
}

// isCertificateRequestApproved returns true if a certificate request has the
// "Approved" condition and no "Denied" conditions; false otherwise.
func isCertificateRequestApproved(csr *certv1.CertificateSigningRequest) bool {
	approved, denied := getCertApprovalCondition(&csr.Status)
	return approved && !denied
}

func getCertApprovalCondition(status *certv1.CertificateSigningRequestStatus) (approved bool, denied bool) {
	for _, c := range status.Conditions {
		if c.Type == certv1.CertificateApproved {
			approved = true
		}
		if c.Type == certv1.CertificateDenied {
			denied = true
		}
	}
	return approved, denied
}
