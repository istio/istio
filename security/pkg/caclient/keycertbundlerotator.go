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

// KeyCertBundleRotator continuously interacts with the upstream CA to maintain the KeyCertBundle valid.
type KeyCertBundleRotator interface {
	// Start the KeyCertBundleRotator loop (blocking call).
	Start(errCh chan<- error)
	// Stop the KeyCertBundleRotator loop.
	Stop()
}

type keyCertBundleRotatorImpl struct {
	// TODO: Support multiple KeyCertBundles.
	certUtil util.CertUtil
	client   CAClient
	keycert  pkiutil.KeyCertBundle
	stopCh   chan bool
	stopped  bool
}

// NewKeyCertBundleRotator creates a new keyCertBundleRotatorImpl instance.
func NewKeyCertBundleRotator(keycert pkiutil.KeyCertBundle, certUtil util.CertUtil, client CAClient) KeyCertBundleRotator {
	return &keyCertBundleRotatorImpl{
		certUtil: certUtil,
		client:   client,
		keycert:  keycert,
		stopCh:   make(chan bool, 1),
		stopped:  true,
	}
}

// Start periodically rotates the KeyCertBundle by interacting with the upstream CA.
// It is a blocking function that should run as a go routine. Not thread safe.
func (c *keyCertBundleRotatorImpl) Start(errCh chan<- error) {
	if !c.stopped {
		errCh <- fmt.Errorf("rotator already started")
		return
	}
	c.stopped = false
	for {
		certBytes, _, _, _ := c.keycert.GetAllPem()
		if len(certBytes) != 0 {
			waitTime, ttlErr := c.certUtil.GetWaitTime(certBytes, time.Now())
			if ttlErr != nil {
				log.Errorf("Error getting TTL from cert: %v. Rotate immediately.", ttlErr)
			} else {
				timer := time.NewTimer(waitTime)
				log.Infof("Will rotate key and cert in %v.", waitTime)
				select {
				case <-c.stopCh:
					return
				case <-timer.C:
					// Continue in the loop.
				}
			}
		}
		log.Infof("Retrieve new key and certs.")
		certBytes, certChainBytes, privateKeyBytes, err := c.client.RetrieveNewKeyCert()
		if err != nil {
			errCh <- fmt.Errorf("error retrieving the key and cert: %v, abort auto rotation", err)
			c.stopped = true
			return
		}
		_, _, _, rootCertBytes := c.keycert.GetAllPem()
		if err = c.keycert.VerifyAndSetAll(certBytes, privateKeyBytes, certChainBytes, rootCertBytes); err != nil {
			errCh <- fmt.Errorf("cannot verify the retrieved key and cert: %v, abort auto rotation", err)
			c.stopped = true
			return
		}
		log.Infof("Successfully retrieved new key and certs.")
	}
}

// Stops the loop. Not thread safe.
func (c *keyCertBundleRotatorImpl) Stop() {
	if !c.stopped {
		c.stopped = true
		c.stopCh <- true
	}
}
