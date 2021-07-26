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

package csrctrl

import (
	"os"
	"strings"
	"time"

	capi "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"

	// +kubebuilder:scaffold:imports
	"istio.io/istio/pkg/test/csrctrl/signer"
	"istio.io/pkg/log"
)

const (
	// Define the root path for signer to store CA and private key files.
	signerRoot = "/tmp/pki/signer/"

	// The duration of the signed certificates
	certificateDuration = 1 * time.Hour
)

var (
	scheme         = runtime.NewScheme()
	loggingOptions = log.DefaultOptions()
	_              = capi.AddToScheme(scheme)
	_              = corev1.AddToScheme(scheme)
)

func RunCSRController(signerNames string, config *rest.Config) {
	// Config Istio log
	if err := log.Configure(loggingOptions); err != nil {
		log.Infof("Unable to configure Istio log error: %v", err)
		os.Exit(-1)
	}
	mgr, err := ctrl.NewManager(config, ctrl.Options{
		Scheme: scheme,
		// disabel the metric server to avoid the port conflicting
		MetricsBindAddress: "0",
	})
	if err != nil {
		log.Infof("Unable to start manager error: %v", err)
		os.Exit(-1)
	}

	arrSingers := strings.Split(signerNames, ",")
	signersMap := make(map[string]*signer.Signer, len(arrSingers))
	for _, signerName := range arrSingers {
		signer, sErr := signer.NewSigner(signerRoot, signerName, certificateDuration)
		if sErr != nil {
			log.Infof("Unable to start signer for [%s], error: %v", signerName, sErr)
			os.Exit(-1)
		}
		signersMap[signerName] = signer
	}

	if err := (&CertificateSigningRequestSigningReconciler{
		Client:      mgr.GetClient(),
		SignerRoot:  signerRoot,
		CtrlCertTTL: certificateDuration,
		Scheme:      mgr.GetScheme(),
		SignerNames: arrSingers,
		Signers:     signersMap,
	}).SetupWithManager(mgr); err != nil {
		log.Infof("Unable to create Controller fro controller CSRSigningReconciler, error: %v", err)
		os.Exit(-1)
	}
	// +kubebuilder:scaffold:builder
	log.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Infof("Problem running manager, error: %v", err)
		os.Exit(-1)
	}
}
