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
	"fmt"
	"os"
	"strings"
	"time"

	"istio.io/istio/pkg/kube"
	// +kubebuilder:scaffold:imports
	"istio.io/istio/pkg/test/csrctrl/signer"
)

const (
	// Define the root path for signer to store CA and private key files.
	signerRoot = "/tmp/pki/signer/"

	// The duration of the signed certificates
	certificateDuration = 1 * time.Hour
)

type SignerRootCert struct {
	Signer   string
	Rootcert string
}

func RunCSRController(signerNames string, stop <-chan struct{}, clients []kube.Client) ([]SignerRootCert, error) {
	arrSigners := strings.Split(signerNames, ",")
	signersMap := make(map[string]*signer.Signer, len(arrSigners))
	var rootCertSignerArr []SignerRootCert
	for _, signerName := range arrSigners {
		signer, err := signer.NewSigner(signerRoot, signerName, certificateDuration)
		if err != nil {
			return nil, fmt.Errorf("unable to start signer for %q: %v", signerName, err)
		}
		signersMap[signerName] = signer
		rootCert, rErr := os.ReadFile(signer.GetRootCerts())
		if rErr != nil {
			return nil, fmt.Errorf("unable to read root cert for signer %q: %v", signerName, err)
		}
		rootCertsForSigner := SignerRootCert{
			Signer:   signerName,
			Rootcert: string(rootCert),
		}
		rootCertSignerArr = append(rootCertSignerArr, rootCertsForSigner)
	}

	for _, cl := range clients {
		signer := NewSigner(cl, signersMap)
		go signer.Run(stop)
		cl.RunAndWait(stop)
		kube.WaitForCacheSync("csr", stop, signer.HasSynced)
	}

	return rootCertSignerArr, nil
}
