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
	"context"
	"fmt"
	"time"

	capi "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"istio.io/istio/pkg/test/csrctrl/signer"
	"istio.io/istio/security/pkg/k8s/chiron"
	"istio.io/istio/security/pkg/pki/util"
	"istio.io/pkg/log"
)

// CertificateSigningRequestSigningReconciler reconciles a CertificateSigningRequest object
type CertificateSigningRequestSigningReconciler struct {
	client.Client
	SignerRoot     string
	CtrlCertTTL    time.Duration
	Scheme         *runtime.Scheme
	SignerNames    []string
	Signers        map[string]*signer.Signer
	appendRootCert bool
}

// +kubebuilder:rbac:groups=certificates.k8s.io,resources=certificatesigningrequests,verbs=get;list;watch
// +kubebuilder:rbac:groups=certificates.k8s.io,resources=certificatesigningrequests/status,verbs=patch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

func (r *CertificateSigningRequestSigningReconciler) Reconcile(_ context.Context, req ctrl.Request) (ctrl.Result, error) {
	var csr capi.CertificateSigningRequest
	if err := r.Client.Get(context.TODO(), req.NamespacedName, &csr); err != nil {
		return ctrl.Result{}, fmt.Errorf("error %q getting CSR", err)
	}

	var exist bool
	_, exist = r.Signers[csr.Spec.SignerName]
	switch {
	case !csr.DeletionTimestamp.IsZero():
		log.Info("CSR has been deleted. Ignoring.")
	case csr.Spec.SignerName == "":
		log.Info("CSR does not have a signer name. Ignoring.")
	case !exist:
		log.Infof("CSR signer name does not match. Ignoring. signer-name: %s", csr.Spec.SignerName)
	case csr.Status.Certificate != nil:
		log.Info("CSR has already been signed. Ignoring.")
	case IsCertificateRequestApproved(&csr):
		log.Info("CSR is not approved, Ignoring.")
	default:
		log.Info("Signing")
		x509cr, err := util.ParsePemEncodedCSR(csr.Spec.Request)
		if err != nil {
			log.Infof("unable to parse csr: %v", err)
			return ctrl.Result{}, nil
		}

		var ok bool
		var signer *signer.Signer
		if signer, ok = r.Signers[csr.Spec.SignerName]; !ok {
			return ctrl.Result{}, fmt.Errorf("error no signer can sign this csr: %v", err)
		}
		requestedLifeTime := signer.CertTTL
		requestedDuration, ok := csr.Annotations[chiron.RequestLifeTimeAnnotationForCertManager]
		if ok {
			duration, err := time.ParseDuration(requestedDuration)
			if err == nil {
				requestedLifeTime = duration
			}
		}
		cert, err := signer.Sign(x509cr, csr.Spec.Usages, requestedLifeTime, r.appendRootCert)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("error auto signing csr: %v", err)
		}
		patch := client.MergeFrom(csr.DeepCopy())
		csr.Status.Certificate = cert
		if err := r.Client.Status().Patch(context.TODO(), &csr, patch); err != nil {
			return ctrl.Result{}, fmt.Errorf("error patching CSR: %v", err)
		}
		log.Infof("The CSR [%s] has been signed", csr.Spec.SignerName)
	}
	return ctrl.Result{}, nil
}

func (r *CertificateSigningRequestSigningReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&capi.CertificateSigningRequest{}).
		Complete(r)
}

// IsCertificateRequestApproved returns true if a certificate request has the
// "Approved" condition and no "Denied" conditions; false otherwise.
func IsCertificateRequestApproved(csr *capi.CertificateSigningRequest) bool {
	approved, denied := GetCertApprovalCondition(&csr.Status)
	return approved && !denied
}

func GetCertApprovalCondition(status *capi.CertificateSigningRequestStatus) (approved bool, denied bool) {
	for _, c := range status.Conditions {
		if c.Type == capi.CertificateApproved {
			approved = true
		}
		if c.Type == capi.CertificateDenied {
			denied = true
		}
	}
	return
}
