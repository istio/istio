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

package main

import (
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	capi "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"

	// +kubebuilder:scaffold:imports
	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/test/csrctrl/controllers"
	"istio.io/istio/pkg/test/csrctrl/signer"
	"istio.io/pkg/log"
)

var (
	signerNames         string
	signerRoot          string
	certificateDuration time.Duration
	debugLogging        bool

	scheme         = runtime.NewScheme()
	loggingOptions = log.DefaultOptions()
	_              = capi.AddToScheme(scheme)
	_              = corev1.AddToScheme(scheme)

	rootCmd = &cobra.Command{
		Use:          "client",
		Short:        "Istio test CSR controller.",
		SilenceUsage: true,
		Long: `Istio test CSR controller used for signing CSR with specified signerName, and this CSR controller can 
support multiple singer names.'
`,
		Args:              cobra.ExactArgs(0),
		PersistentPreRunE: configureLogging,
		Run: func(cmd *cobra.Command, args []string) {
			mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
				Scheme: scheme,
				Port:   9443,
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
			if err := (&controllers.CertificateSigningRequestSigningReconciler{
				Client:        mgr.GetClient(),
				SignerRoot:    signerRoot,
				CtrlCertTTL:   certificateDuration,
				Scheme:        mgr.GetScheme(),
				SignerNames:   arrSingers,
				Signers:       signersMap,
				EventRecorder: mgr.GetEventRecorderFor("CSRSigningReconciler"),
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
		},
	}
)

func configureLogging(_ *cobra.Command, _ []string) error {
	if err := log.Configure(loggingOptions); err != nil {
		return err
	}
	return nil
}

func init() {
	rootCmd.PersistentFlags().StringVar(&signerNames, "signer-names", "istio.io/foo,istio.io/bar",
		"Only sign CSR with this .spec.signerName, and multiple signer names can be splitted all signer names by ',', such as 'foo,bar'")
	rootCmd.PersistentFlags().StringVar(&signerRoot, "signer-root-path", "/tmp/pki/signer/", "Define the root path for signer to store CA and private key files.")
	rootCmd.PersistentFlags().DurationVar(&certificateDuration, "certificate-duration", time.Hour, "The duration of the signed certificates.")
	rootCmd.PersistentFlags().BoolVar(&debugLogging, "debug-logging", true, "Enable debug logging.")

	loggingOptions.AttachCobraFlags(rootCmd)
	cmd.AddFlags(rootCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Infof("Error: %v", err)
		os.Exit(-1)
	}
}
