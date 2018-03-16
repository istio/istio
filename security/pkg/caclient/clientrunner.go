// Copyright 2018 Istio Authors
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
	"fmt"
	"time"

	"istio.io/istio/pkg/log"
	pkiutil "istio.io/istio/security/pkg/pki/util"
	"istio.io/istio/security/pkg/util"
)

// ClientRunner interacts with the upstream CA to maintain the KeyCertBundle valid.
// TODO: Consider a better name.
type ClientRunner interface {
	// Run the ClientRunner loop (blocking call)
	Run(stopCh <-chan struct{}, errCh chan<- error)
}

type clientRunner struct {
	// TODO: Support multiple KeyCertBundles.
	keycert  pkiutil.KeyCertBundle
	certUtil util.CertUtil
	client   CAClient
}

// NewClientRunner creates a new clientReunner instance.
func NewClientRunner(keycert pkiutil.KeyCertBundle, certUtil util.CertUtil, client CAClient) ClientRunner {
	return &clientRunner{
		keycert:  keycert,
		certUtil: certUtil,
		client:   client,
	}
}

// Run periodically rotates the key/cert of the Istio CA by interacting with the upstream Istio CA.
// It is a blocking function that should run as a go routine.
func (c *clientRunner) Run(stopCh <-chan struct{}, errCh chan<- error) {
	for {
		certBytes, _, _, _ := c.keycert.GetAllPem()
		if len(certBytes) != 0 {
			waitTime, ttlErr := c.certUtil.GetWaitTime(certBytes, time.Now())
			if ttlErr == nil {
				timer := time.NewTimer(waitTime)
				log.Infof("Will rotate key and cert in %v.", waitTime)
				select {
				case <-stopCh:
					return
				case <-timer.C:
					// Continue in the loop.
				}
			} else {
				log.Errorf("Error getting TTL from cert: %v. Rotate immediately.", ttlErr)
			}
		}
		log.Infof("Retrieve new key and certs.")
		certBytes, certChainBytes, privateKeyBytes, err := c.client.RetrieveNewKeyCert()
		if err != nil {
			errCh <- fmt.Errorf("error retrieving the key and cert: %v, abort auto rotation", err)
			return
		}
		_, _, _, rootCertBytes := c.keycert.GetAllPem()
		if err = c.keycert.VerifyAndSetAll(certBytes, privateKeyBytes, certChainBytes, rootCertBytes); err != nil {
			errCh <- fmt.Errorf("cannot verify the retrieved key and cert: %v, abort auto rotation", err)
			return
		}
		log.Infof("Successfully retrieved new key and certs.")
	}
}
