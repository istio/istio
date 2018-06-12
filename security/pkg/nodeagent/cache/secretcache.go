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

// Package cache is the in-memory secret store.
package cache

import (
	"sync"
	"sync/atomic"
	"time"

	"istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/pki/util"
)

// The size of a private key for a leaf certificate.
const keySize = 2048

// SecretManager defines secrets management interface which is used by SDS.
type SecretManager interface {
	GetSecret(proxyID, token string) (*SecretItem, error)
}

// CAClient interface defines the clients need to implement to talk to CA for CSR.
// TODO(quanlin): CAClient here is a placeholder, will move it to separated pkg.
type CAClient interface {
	CSRSign(csrPEM []byte /*PEM-encoded certificate request*/, subjectID string, certValidTTLInSec int64) ([]byte /*PEM-encoded certificate chain*/, error)
}

// SecretItem is the cached item in in-memory secret store.
type SecretItem struct {
	CertificateChain []byte
	PrivateKey       []byte
	token            string
	lastUsedTime     time.Time
	createdTime      time.Time
}

// SecretCache is the in-memory cache for secrets.
type SecretCache struct {
	// secrets map is the cache for secrets.
	// map key is Envoy instance ID, map value is secretItem.
	secrets        sync.Map
	rotationTicker *time.Ticker
	caClient       CAClient
	closing        chan bool

	// Cached secret will be removed from cache if (time.now - secretItem.lastUsedTime >= evictionDuration), this prevents cache growing indefinitely.
	evictionDuration time.Duration

	// Key rotation job running interval.
	rotationInterval time.Duration

	// secret TTL.
	secretTTL time.Duration

	// How may times that key rotation job has detected secret change happened, used in unit test.
	secretChangedCount uint64
}

// NewSecretCache creates a new secret cache.
func NewSecretCache(cl CAClient, secretTTL, rotationInterval, evictionDuration time.Duration) *SecretCache {
	ret := &SecretCache{
		caClient:         cl,
		rotationInterval: rotationInterval,
		evictionDuration: evictionDuration,
		secretTTL:        secretTTL,
		closing:          make(chan bool, 1),
	}

	atomic.StoreUint64(&ret.secretChangedCount, 0)
	go ret.keyCertRotationJob()
	return ret
}

// GetSecret gets secret from cache, this function is called by SDS.FetchSecret,
// Since credential passing from client may change, regenerate secret every time
// instread of reading from cache.
func (sc *SecretCache) GetSecret(proxyID, token string) (*SecretItem, error) {
	ns, err := sc.generateSecret(token, time.Now())
	if err != nil {
		log.Errorf("Failed to generate secret for proxy %q: %v", proxyID, err)
		return nil, err
	}

	sc.secrets.Store(proxyID, *ns)
	return ns, nil
}

// Close shuts down the secret cache.
func (sc *SecretCache) Close() {
	sc.closing <- true
}

func (sc *SecretCache) keyCertRotationJob() {
	// Wake up once in a while and refresh stale items.
	sc.rotationTicker = time.NewTicker(sc.rotationInterval)
	for {
		select {
		case now := <-sc.rotationTicker.C:
			sc.rotate(now)
		case <-sc.closing:
			sc.rotationTicker.Stop()
			return
		}
	}
}

func (sc *SecretCache) rotate(t time.Time) {
	sc.secrets.Range(func(key interface{}, value interface{}) bool {
		proxyID := key.(string)
		now := time.Now()

		e := value.(SecretItem)

		// Remove from cache if the secret hasn't been used for a while, this prevent the cache growing indefinitely.
		if now.After(e.lastUsedTime.Add(sc.evictionDuration)) {
			sc.secrets.Delete(proxyID)
			return true
		}

		// Re-generate secret if it's expired.
		if sc.checkExpired(&e) {
			go func() {
				ns, err := sc.generateSecret(e.token, now)
				if err != nil {
					log.Errorf("Failed to generate secret for proxy %q: %v", proxyID, err)
					return
				}

				sc.secrets.Store(proxyID, *ns)

				//TODO(quanlin): push new secret to proxy.

				atomic.AddUint64(&sc.secretChangedCount, 1)
			}()
		}

		return true
	})
}

func (sc *SecretCache) generateSecret(token string, t time.Time) (*SecretItem, error) {
	options := util.CertOptions{
		Host:       "", //TODO(quanlin): figure out what to use here.
		RSAKeySize: keySize,
	}

	// Generate the cert/key, send CSR to CA.
	csrPEM, keyPEM, err := util.GenCSR(options)
	if err != nil {
		return nil, err
	}

	certChainPER, err := sc.caClient.CSRSign(csrPEM, token, int64(sc.secretTTL.Seconds()))
	if err != nil {
		return nil, err
	}

	return &SecretItem{
		CertificateChain: certChainPER,
		PrivateKey:       keyPEM,
		token:            token,
		lastUsedTime:     t,
		createdTime:      t,
	}, nil
}

func (sc *SecretCache) checkExpired(s *SecretItem) bool {
	now := time.Now()
	if now.After(s.createdTime.Add(sc.secretTTL)) {
		return true
	}

	// TODO(quanlin), check if token has expired.

	return false
}
