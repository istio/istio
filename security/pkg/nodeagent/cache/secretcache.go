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
	ca "istio.io/istio/security/pkg/nodeagent/caClient"
	"istio.io/istio/security/pkg/nodeagent/sds"
	"istio.io/istio/security/pkg/pki/util"
)

// The size of a private key for a leaf certificate.
const keySize = 2048

// Right now always skip the check, since key rotation job
// checks token expire only when cert has expired;
// since token's TTL is much shorter than the cert, we could
// skip the check in normal cases.
// The flag is used in unit test.
var skipTokenExpireCheck = true

// Options provides all of the configuration parameters for secret cache.
type Options struct {
	// secret TTL.
	SecretTTL time.Duration

	// Secret rotation job running interval.
	RotationInterval time.Duration

	// Secret eviction duration.
	EvictionDuration time.Duration
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
}

// NewSecretCache creates a new secret cache.
func NewSecretCache(cl ca.Client, options Options) *SecretCache {
	ret := &SecretCache{
		caClient:         cl,
		rotationInterval: options.RotationInterval,
		evictionDuration: options.EvictionDuration,
		secretTTL:        options.SecretTTL,
	}

	atomic.StoreUint64(&ret.secretChangedCount, 0)
	go ret.keyCertRotationJob()
	return ret
}

// GetSecret gets secret from cache, this function is called by SDS.FetchSecret,
// Since credential passing from client may change, regenerate secret every time
// instread of reading from cache.
func (sc *SecretCache) GetSecret(ctx context.Context, proxyID, spiffeID, token string) (*sds.SecretItem, error) {
	ns, err := sc.generateSecret(ctx, token, spiffeID, time.Now())
	if err != nil {
		log.Errorf("Failed to generate secret for proxy %q: %v", proxyID, err)
		return nil, err
	}

	sc.secrets.Store(proxyID, *ns)
	return ns, nil
}

// Close shuts down the secret cache.
func (sc *SecretCache) Close() {
	sc.rotationTicker.Stop()
}

func (sc *SecretCache) keyCertRotationJob() {
	// Wake up once in a while and refresh stale items.
	sc.rotationTicker = time.NewTicker(sc.rotationInterval)
	for {
		select {
		case now := <-sc.rotationTicker.C:
			sc.rotate(now)
		}
	}
}

func (sc *SecretCache) rotate(t time.Time) {
	sc.secrets.Range(func(key interface{}, value interface{}) bool {
		proxyID := key.(string)
		now := time.Now()

		e := value.(sds.SecretItem)

		// Remove stale secrets from cache, this prevent the cache growing indefinitely.
		if now.After(e.CreatedTime.Add(sc.evictionDuration)) {
			sc.secrets.Delete(proxyID)
			return true
		}

		// Re-generate secret if it's expired.
		if sc.shouldRefresh(&e) {
			go func() {
				if sc.isTokenExpired(&e) {
					// Send the notification to close the stream connection if both cert and token have expired.
					if err := sds.NotifyProxy(proxyID, nil /*nil indicates close the streaming connection to proxy*/); err != nil {
						log.Errorf("Failed to notify for proxy %q: %v", proxyID, err)
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

				if err := sds.NotifyProxy(proxyID, ns); err != nil {
					log.Errorf("Failed to notify secret change for proxy %q: %v", proxyID, err)
				}

			}()
		}

		return true
	})
}

func (sc *SecretCache) generateSecret(ctx context.Context, token, spiffeID string, t time.Time) (*sds.SecretItem, error) {
	options := util.CertOptions{
		Host:       spiffeID,
		RSAKeySize: keySize,
	}

	// Generate the cert/key, send CSR to CA.
	csrPEM, keyPEM, err := util.GenCSR(options)
	if err != nil {
		return nil, err
	}

	certChainPER, err := sc.caClient.CSRSign(ctx, csrPEM, token, int64(sc.secretTTL.Seconds()))
	if err != nil {
		return nil, err
	}

	return &sds.SecretItem{
		CertificateChain: certChainPER,
		PrivateKey:       keyPEM,
		SpiffeID:         spiffeID,
		Token:            token,
		CreatedTime:      t,
	}, nil
}

func (sc *SecretCache) shouldRefresh(s *sds.SecretItem) bool {
	// TODO(quanlin), check if cert has expired.
	// May need to be more accurate - reserve some grace period to
	// make sure now() <= TTL - envoy_reconnect_delay - CSR_round_trip delay
	now := time.Now()
	if now.After(s.CreatedTime.Add(sc.secretTTL)) {
		return true
	}

	return false
}

func (sc *SecretCache) isTokenExpired(s *sds.SecretItem) bool {
	if skipTokenExpireCheck {
		return true
	}
	// TODO(quanlin), check if token has expired.
	return false
}
