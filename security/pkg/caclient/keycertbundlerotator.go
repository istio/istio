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
	"sync"
	"time"

	"istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/caclient/protocol"
	pkiutil "istio.io/istio/security/pkg/pki/util"
	"istio.io/istio/security/pkg/platform"
	"istio.io/istio/security/pkg/util"
)

// NewKeyCertBundleRotator is constructor for keyCertBundleRotatorImpl based on the provided configuration.
func NewKeyCertBundleRotator(cfg *Config, keyCertBundle pkiutil.KeyCertBundle) (*KeyCertBundleRotator, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil configuration passed")
	}
	pc, err := platform.NewClient(cfg.Env, cfg.RootCertFile, cfg.KeyFile, cfg.CertChainFile, cfg.CAAddress)
	if err != nil {
		return nil, err
	}
	dial, err := pc.GetDialOptions()
	if err != nil {
		return nil, err
	}
	grpcConn, err := protocol.NewGrpcConnection(cfg.CAAddress, dial)
	if err != nil {
		return nil, err
	}
	cac, err := NewCAClient(pc, grpcConn, cfg.CSRMaxRetries, cfg.CSRInitialRetrialInterval)
	if err != nil {
		return nil, err
	}
	return &KeyCertBundleRotator{
		certUtil:  util.NewCertUtil(cfg.CSRGracePeriodPercentage),
		retriever: cac,
		keycert:   keyCertBundle,
		stopCh:    make(chan bool, 1),
		stopped:   true,
	}, nil
}

// KeyCertRetriever is the interface responsible for retrieve new key and certificate from upstream CA.
type KeyCertRetriever interface {
	Retrieve(opt *pkiutil.CertOptions) (newCert, certChain, privateKey []byte, err error)
}

// KeyCertBundleRotator automatically updates the key and cert bundle by interacting with upstream CA.
type KeyCertBundleRotator struct {
	// TODO: Support multiple KeyCertBundles.
	certUtil     util.CertUtil
	keycert      pkiutil.KeyCertBundle
	stopCh       chan bool
	stopped      bool
	retriever    KeyCertRetriever
	stoppedMutex sync.Mutex
}

// Start periodically rotates the KeyCertBundle by interacting with the upstream CA.
// It is a blocking function that should run as a go routine. Thread safe.
func (c *KeyCertBundleRotator) Start(errCh chan<- error) {
	c.stoppedMutex.Lock()
	if !c.stopped {
		errCh <- fmt.Errorf("rotator already started")
		c.stoppedMutex.Unlock()
		return
	}
	c.stopped = false
	c.stoppedMutex.Unlock()

	// Make sure we mark rotator stopped after this method finishes.
	defer func() {
		c.stoppedMutex.Lock()
		c.stopped = true
		c.stoppedMutex.Unlock()
	}()

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
		co, coErr := c.keycert.CertOptions()
		if coErr != nil {
			err := fmt.Errorf("failed to extact CertOptions from bundle: %v, abort auto rotation", coErr)
			log.Errora(err)
			errCh <- err
			return
		}
		certBytes, certChainBytes, privKeyBytes, rErr := c.retriever.Retrieve(co)
		if rErr != nil {
			err := fmt.Errorf("error retrieving the key and cert: %v, abort auto rotation", rErr)
			log.Errora(err)
			errCh <- err
			return
		}
		_, _, _, rootCertBytes := c.keycert.GetAllPem()
		if vErr := c.keycert.VerifyAndSetAll(certBytes, privKeyBytes, certChainBytes, rootCertBytes); vErr != nil {
			err := fmt.Errorf("cannot verify the retrieved key and cert: %v, abort auto rotation", vErr)
			log.Errora(err)
			errCh <- err
			return
		}
		log.Infof("Successfully retrieved new key and certs.")
	}
}

// Stop stops the loop. Thread safe.
func (c *KeyCertBundleRotator) Stop() {
	c.stoppedMutex.Lock()
	if !c.stopped {
		c.stopped = true
		c.stopCh <- true
	}
	c.stoppedMutex.Unlock()
}
