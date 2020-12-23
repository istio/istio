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

package adsc

import (
	"context"
	"crypto/tls"
	"io/ioutil"

	"istio.io/istio/security/pkg/nodeagent/cache"
	"istio.io/pkg/log"
)

func getClientCertFn(config *Config) func(requestInfo *tls.CertificateRequestInfo) (*tls.Certificate, error) {
	if config.SecretManager != nil {
		return func(requestInfo *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			tok, err := ioutil.ReadFile(config.JWTPath)
			if err != nil {
				log.Infof("Failed to get credential token in agent: %v", err)
				tok = []byte("")
			}

			key, err := config.SecretManager.GenerateSecret(context.Background(), "agent",
				cache.WorkloadKeyCertResourceName, string(tok))
			if err != nil {
				return nil, err
			}
			clientCert, err := tls.X509KeyPair(key.CertificateChain, key.PrivateKey)
			if err != nil {
				return nil, err
			}
			return &clientCert, nil
		}
	}
	if config.CertDir != "" {
		return func(requestInfo *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			certName := config.CertDir + "/cert-chain.pem"
			clientCert, err := tls.LoadX509KeyPair(certName, config.CertDir+"/key.pem")
			if err != nil {
				return nil, err
			}
			return &clientCert, nil
		}
	}

	return nil
}
