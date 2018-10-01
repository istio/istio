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
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"istio.io/istio/pkg/log"
	ca "istio.io/istio/security/pkg/nodeagent/caclient"
	"istio.io/istio/security/pkg/nodeagent/model"
	"istio.io/istio/security/pkg/pki/util"
)

const (
	// The size of a private key for a leaf certificate.
	keySize = 2048

	// max retry number to wait CSR response come back to parse root cert from it.
	maxRetryNum = 5

	// initial retry wait time duration when waiting root cert is available.
	retryWaitDuration = 200 * time.Millisecond

	// RootCertReqResourceName is resource name of discovery request for root certificate.
	RootCertReqResourceName = "ROOTCA"
)

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
	// GenerateSecret generates new secret and cache the secret.
	GenerateSecret(ctx context.Context, proxyID, spiffeID, token string) (*model.SecretItem, error)

	// SecretExist checks if secret already existed.
	SecretExist(proxyID, spiffeID, token, version string) bool
}

// ConnKey is the key of one SDS connection.
type ConnKey struct {
	ProxyID string

	// ResourceName of SDS request, get from SDS.DiscoveryRequest.ResourceName
	// Current it's `ROOTCA` for root cert request, and spiffeID for normal key/cert request.
	ResourceName string
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
	notifyCallback func(proxyID string, resourceName string, secret *model.SecretItem) error
	// Right now always skip the check, since key rotation job checks token expire only when cert has expired;
	// since token's TTL is much shorter than the cert, we could skip the check in normal cases.
	// The flag is used in unit test, use uint32 instead of boolean because there is no atomic boolean
	// type in golang, atomic is needed to avoid racing condition in unit test.
	skipTokenExpireCheck uint32

	// close channel.
	closing chan bool

	rootCertMutex *sync.Mutex
	rootCert      []byte
}

// NewSecretCache creates a new secret cache.
func NewSecretCache(cl ca.Client, notifyCb func(string, string, *model.SecretItem) error, options Options) *SecretCache {
	ret := &SecretCache{
		caClient:         cl,
		closing:          make(chan bool),
		evictionDuration: options.EvictionDuration,
		notifyCallback:   notifyCb,
		rootCertMutex:    &sync.Mutex{},
		rotationInterval: options.RotationInterval,
		secretTTL:        options.SecretTTL,
	}

	atomic.StoreUint64(&ret.secretChangedCount, 0)
	atomic.StoreUint32(&ret.skipTokenExpireCheck, 1)
	go ret.keyCertRotationJob()
	return ret
}

// GenerateSecret generates new secret and cache the secret, this function is called by SDS.StreamSecrets
// and SDS.FetchSecret. Since credential passing from client may change, regenerate secret every time
// instead of reading from cache.
func (sc *SecretCache) GenerateSecret(ctx context.Context, proxyID, resourceName, token string) (*model.SecretItem, error) {
	var ns *model.SecretItem
	key := ConnKey{
		ProxyID:      proxyID,
		ResourceName: resourceName,
	}

	if resourceName != RootCertReqResourceName {
		// Request for normal key/cert pair.
		ns, err := sc.generateSecret(ctx, token, resourceName, time.Now())
		if err != nil {
			log.Errorf("Failed to generate secret for proxy %q: %v", proxyID, err)
			return nil, err
		}

		sc.secrets.Store(key, *ns)
		return ns, nil
	}

	// If request is for root certificate,
	// retry since rootCert may be empty until there is CSR response returned from CA.
	if sc.rootCert == nil {
		wait := retryWaitDuration
		retryNum := 0
		for ; retryNum < maxRetryNum; retryNum++ {
			time.Sleep(retryWaitDuration)
			if sc.rootCert != nil {
				break
			}

			wait *= 2
		}
	}

	if sc.rootCert == nil {
		log.Errorf("Failed to get root cert for proxy %q", proxyID)
		return nil, errors.New("faied to get root cert")

	}

	t := time.Now()
	ns = &model.SecretItem{
		ResourceName: resourceName,
		RootCert:     sc.rootCert,
		Token:        token,
		CreatedTime:  t,
		Version:      t.String(),
	}
	sc.secrets.Store(key, *ns)
	return ns, nil
}

// SecretExist checks if secret already existed.
func (sc *SecretCache) SecretExist(proxyID, resourceName, token, version string) bool {
	key := ConnKey{
		ProxyID:      proxyID,
		ResourceName: resourceName,
	}
	val, exist := sc.secrets.Load(key)
	if !exist {
		return false
	}

	e := val.(model.SecretItem)
	if e.ResourceName == resourceName && e.Token == token && e.Version == version {
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

	sc.secrets.Range(func(k interface{}, v interface{}) bool {
		key := k.(ConnKey)

		// skip the refresh if cached item is root cert.
		if key.ResourceName == RootCertReqResourceName {
			return true
		}

		proxyID := key.ProxyID
		now := time.Now()

		e := v.(model.SecretItem)

		// Remove stale secrets from cache, this prevent the cache growing indefinitely.
		if now.After(e.CreatedTime.Add(sc.evictionDuration)) {
			sc.secrets.Delete(key)
			return true
		}

		// Re-generate secret if it's expired.
		if sc.shouldRefresh(&e) {
			go func() {
				if sc.isTokenExpired(&e) {
					log.Debugf("Token for %q expired for proxy %q", e.ResourceName, proxyID)

					if sc.notifyCallback != nil {
						// Send the notification to close the stream connection if both cert and token have expired.
						if err := sc.notifyCallback(key.ProxyID, key.ResourceName, nil /*nil indicates close the streaming connection to proxy*/); err != nil {
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
				ns, err := sc.generateSecret(context.Background(), e.Token, e.ResourceName, now)
				if err != nil {
					log.Errorf("Failed to generate secret for proxy %q: %v", proxyID, err)
					return
				}

				sc.secrets.Store(key, *ns)

				atomic.AddUint64(&sc.secretChangedCount, 1)

				if sc.notifyCallback != nil {
					if err := sc.notifyCallback(proxyID, key.ResourceName, ns); err != nil {
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

	certChain := []byte{}
	for _, c := range certChainPEM {
		certChain = append(certChain, []byte(c)...)
	}

	len := len(certChainPEM)
	// Leaf cert is element '0'. Root cert is element 'n'.
	if sc.rootCert == nil || bytes.Compare(sc.rootCert, []byte(certChainPEM[len-1])) != 0 {
		sc.rootCertMutex.Lock()
		sc.rootCert = []byte(certChainPEM[len-1])
		sc.rootCertMutex.Unlock()

		// TODO(quanlin): notify proxy about root cert change.
	}

	return &model.SecretItem{
		CertificateChain: certChain,
		PrivateKey:       keyPEM,
		ResourceName:     spiffeID,
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
