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
	"context"
	"sync"
	"sync/atomic"
	"time"

	"istio.io/istio/pkg/log"
	ca "istio.io/istio/security/pkg/nodeagent/caclient"
	"istio.io/istio/security/pkg/nodeagent/model"
	"istio.io/istio/security/pkg/pki/util"
)

// The size of a private key for a leaf certificate.
const keySize = 2048

// Options provides all of the configuration parameters for secret cache.
type Options struct {
	// secret TTL.
	SecretTTL time.Duration

	// Secret rotation job running interval.
	RotationInterval time.Duration

	// Secret eviction duration.
	EvictionDuration time.Duration
}

// SecretManager defines secrets management interface which is used by SDS.
type SecretManager interface {
	// GetSecret generates new secret and cache the secret.
	GetSecret(ctx context.Context, proxyID, spiffeID, token string) (*model.SecretItem, error)

	// SecretExist checks if secret already existed.
	SecretExist(proxyID, spiffeID, token, version string) bool
}

// SecretCache is the in-memory cache for secrets.
type SecretCache struct {
	// secrets map is the cache for secrets.
	// map key is Envoy instance ID, map value is secretItem.
	secrets        sync.Map
	rotationTicker *time.Ticker
	caClient       ca.Client

	// Cached secret will be removed from cache if (time.now - secretItem.CreatedTime >= evictionDuration), this prevents cache growing indefinitely.
	evictionDuration time.Duration

	// Key rotation job running interval.
	rotationInterval time.Duration

	// secret TTL.
	secretTTL time.Duration

	// How may times that key rotation job has detected secret change happened, used in unit test.
	secretChangedCount uint64

	// callback function to invoke when detecting secret change.
	notifyCallback func(string, *model.SecretItem) error
	// Right now always skip the check, since key rotation job checks token expire only when cert has expired;
	// since token's TTL is much shorter than the cert, we could skip the check in normal cases.
	// The flag is used in unit test, use uint32 instead of boolean because there is no atomic boolean
	// type in golang, atomic is needed to avoid racing condition in unit test.
	skipTokenExpireCheck uint32

	// close channel.
	closing chan bool
}

// NewSecretCache creates a new secret cache.
func NewSecretCache(cl ca.Client, notifyCb func(string, *model.SecretItem) error, options Options) *SecretCache {
	ret := &SecretCache{
		caClient:         cl,
		rotationInterval: options.RotationInterval,
		evictionDuration: options.EvictionDuration,
		secretTTL:        options.SecretTTL,
		notifyCallback:   notifyCb,
		closing:          make(chan bool),
	}

	atomic.StoreUint64(&ret.secretChangedCount, 0)
	atomic.StoreUint32(&ret.skipTokenExpireCheck, 1)
	go ret.keyCertRotationJob()
	return ret
}

// GetSecret gets secret from cache, this function is called by SDS.FetchSecret,
// Since credential passing from client may change, regenerate secret every time
// instread of reading from cache.
func (sc *SecretCache) GetSecret(ctx context.Context, proxyID, spiffeID, token string) (*model.SecretItem, error) {
	ns, err := sc.generateSecret(ctx, token, spiffeID, time.Now())
	if err != nil {
		log.Errorf("Failed to generate secret for proxy %q: %v", proxyID, err)
		return nil, err
	}

	sc.secrets.Store(proxyID, *ns)
	return ns, nil
}

// SecretExist checks if secret already existed.
func (sc *SecretCache) SecretExist(proxyID, spiffeID, token, version string) bool {
	val, exist := sc.secrets.Load(proxyID)
	if !exist {
		return false
	}

	e := val.(model.SecretItem)
	if e.SpiffeID == spiffeID && e.Token == token && e.Version == version {
		return true
	}

	return false
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
			if sc.rotationTicker != nil {
				sc.rotationTicker.Stop()
			}
		}
	}
}

func (sc *SecretCache) rotate(t time.Time) {
	log.Debug("Refresh job running")

	sc.secrets.Range(func(key interface{}, value interface{}) bool {
		proxyID := key.(string)
		now := time.Now()

		e := value.(model.SecretItem)

		// Remove stale secrets from cache, this prevent the cache growing indefinitely.
		if now.After(e.CreatedTime.Add(sc.evictionDuration)) {
			sc.secrets.Delete(proxyID)
			return true
		}

		// Re-generate secret if it's expired.
		if sc.shouldRefresh(&e) {
			go func() {
				if sc.isTokenExpired(&e) {
					log.Debugf("Token for %q expired", e.SpiffeID)

					if sc.notifyCallback != nil {
						// Send the notification to close the stream connection if both cert and token have expired.
						if err := sc.notifyCallback(proxyID, nil /*nil indicates close the streaming connection to proxy*/); err != nil {
							log.Errorf("Failed to notify for proxy %q: %v", proxyID, err)
						}
					} else {
						log.Warnf("secret cache notify callback isn't set")
					}

					return
				}

				// If token is still valid, re-generated the secret and push change to proxy.
				// Most likey this code path may not necessary, since TTL of cert is much longer than token.
				// When cert has expired, we could make it simple by assuming token has already expired.
				ns, err := sc.generateSecret(context.Background(), e.Token, e.SpiffeID, now)
				if err != nil {
					log.Errorf("Failed to generate secret for proxy %q: %v", proxyID, err)
					return
				}

				sc.secrets.Store(proxyID, *ns)

				atomic.AddUint64(&sc.secretChangedCount, 1)

				if sc.notifyCallback != nil {
					if err := sc.notifyCallback(proxyID, ns); err != nil {
						log.Errorf("Failed to notify secret change for proxy %q: %v", proxyID, err)
					}
				} else {
					log.Warnf("secret cache notify callback isn't set")
				}

			}()
		}

		return true
	})
}

func (sc *SecretCache) generateSecret(ctx context.Context, token, spiffeID string, t time.Time) (*model.SecretItem, error) {
	options := util.CertOptions{
		Host:       spiffeID,
		RSAKeySize: keySize,
	}

	// Generate the cert/key, send CSR to CA.
	csrPEM, keyPEM, err := util.GenCSR(options)
	if err != nil {
		log.Errorf("Failed to generated key cert for %q: %v", spiffeID, err)
		return nil, err
	}

	certChainPEM, err := sc.caClient.CSRSign(ctx, csrPEM, token, int64(sc.secretTTL.Seconds()))
	if err != nil {
		log.Errorf("Failed to sign cert for %q: %v", spiffeID, err)
		return nil, err
	}

	return &model.SecretItem{
		CertificateChain: certChainPEM,
		PrivateKey:       keyPEM,
		SpiffeID:         spiffeID,
		Token:            token,
		CreatedTime:      t,
		Version:          t.String(),
	}, nil
}

func (sc *SecretCache) shouldRefresh(s *model.SecretItem) bool {
	// TODO(quanlin), check if cert has expired.
	// May need to be more accurate - reserve some grace period to
	// make sure now() <= TTL - envoy_reconnect_delay - CSR_round_trip delay
	now := time.Now()
	if now.After(s.CreatedTime.Add(sc.secretTTL)) {
		return true
	}

	return false
}

func (sc *SecretCache) isTokenExpired(s *model.SecretItem) bool {
	if atomic.LoadUint32(&sc.skipTokenExpireCheck) == 1 {
		return true
	}
	// TODO(quanlin), check if token has expired.
	return false
}
