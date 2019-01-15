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
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gogo/status"
	"google.golang.org/grpc/codes"

	"istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/nodeagent/model"
	"istio.io/istio/security/pkg/nodeagent/plugin"
	"istio.io/istio/security/pkg/nodeagent/secretfetcher"
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

	// identityTemplate is the format template of identity in the CSR request.
	identityTemplate = "spiffe://%s/ns/%s/sa/%s"

	// For REST APIs between envoy->nodeagent, default value of 1s is used.
	envoyDefaultTimeoutInMilliSec = 1000

	// initialBackOffIntervalInMilliSec is the initial backoff time interval when hitting non-retryable error in CSR request.
	initialBackOffIntervalInMilliSec = 50
)

type k8sJwtPayload struct {
	Sub string `json:"sub"`
}

// Options provides all of the configuration parameters for secret cache.
type Options struct {
	// secret TTL.
	SecretTTL time.Duration

	// secret should be refreshed before it expired, SecretRefreshGraceDuration is the grace period;
	// secret should be refreshed if time.Now.After(secret.CreateTime + SecretTTL - SecretRefreshGraceDuration)
	SecretRefreshGraceDuration time.Duration

	// Key rotation job running interval.
	RotationInterval time.Duration

	// Cached secret will be removed from cache if (time.now - secretItem.CreatedTime >= evictionDuration), this prevents cache growing indefinitely.
	EvictionDuration time.Duration

	// TrustDomain corresponds to the trust root of a system.
	// https://github.com/spiffe/spiffe/blob/master/standards/SPIFFE-ID.md#21-trust-domain
	TrustDomain string

	// authentication provider specific plugins.
	Plugins []plugin.Plugin
}

// SecretManager defines secrets management interface which is used by SDS.
type SecretManager interface {
	// GenerateSecret generates new secret and cache the secret.
	GenerateSecret(ctx context.Context, proxyID, resourceName, token string) (*model.SecretItem, error)

	// SecretExist checks if secret already existed.
	SecretExist(proxyID, resourceName, token, version string) bool
}

// ConnKey is the key of one SDS connection.
type ConnKey struct {
	ProxyID string

	// ResourceName of SDS request, get from SDS.DiscoveryRequest.ResourceName
	// Current it's `ROOTCA` for root cert request, and 'default' for normal key/cert request.
	ResourceName string
}

// SecretCache is the in-memory cache for secrets.
type SecretCache struct {
	// secrets map is the cache for secrets.
	// map key is Envoy instance ID, map value is secretItem.
	secrets        sync.Map
	rotationTicker *time.Ticker
	fetcher        *secretfetcher.SecretFetcher

	// configOptions includes all configurable params for the cache.
	configOptions Options

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
func NewSecretCache(fetcher *secretfetcher.SecretFetcher, notifyCb func(string, string, *model.SecretItem) error, options Options) *SecretCache {
	ret := &SecretCache{
		fetcher:        fetcher,
		closing:        make(chan bool),
		notifyCallback: notifyCb,
		rootCertMutex:  &sync.Mutex{},
		configOptions:  options,
	}

	fetcher.DeleteCache = ret.DeleteK8sSecret
	fetcher.UpdateCache = ret.UpdateK8sSecret

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

// DeleteK8sSecret deletes all entries that match secretName. This is called when a K8s secret
// for ingress gateway is deleted.
func (sc *SecretCache) DeleteK8sSecret(secretName string) {
	wg := sync.WaitGroup{}
	sc.secrets.Range(func(k interface{}, v interface{}) bool {
		key := k.(ConnKey)

		if key.ResourceName == secretName {
			proxyID := key.ProxyID
			sc.secrets.Delete(key)
			wg.Add(1)
			go func() {
				defer wg.Done()
				if sc.notifyCallback != nil {
					if err := sc.notifyCallback(proxyID, secretName, nil /*nil indicates close the streaming connection to proxy*/); err != nil {
						log.Errorf("Failed to notify secret change for proxy %q: %v", proxyID, err)
					}
				} else {
					log.Warnf("secret cache notify callback isn't set")
				}
			}()
			// Currently only one ingress gateway is running, therefore there is at most one cache entry.
			// Stop the iteration once we have deleted that cache entry.
			return false
		}
		return true
	})
	wg.Wait()
}

// UpdateK8sSecret updates all entries that match secretName. This is called when a K8s secret
// for ingress gateway is updated.
func (sc *SecretCache) UpdateK8sSecret(secretName string, ns model.SecretItem) {
	secretMap := map[ConnKey]*model.SecretItem{}
	wg := sync.WaitGroup{}
	sc.secrets.Range(func(k interface{}, v interface{}) bool {
		key := k.(ConnKey)
		oldSecret := v.(model.SecretItem)
		if key.ResourceName == secretName {
			proxyID := key.ProxyID
			wg.Add(1)
			go func() {
				defer wg.Done()

				newSecret := &model.SecretItem{
					CertificateChain: ns.CertificateChain,
					PrivateKey:       ns.PrivateKey,
					ResourceName:     secretName,
					Token:            oldSecret.Token,
					CreatedTime:      ns.CreatedTime,
					Version:          ns.Version,
				}
				secretMap[key] = newSecret
				if sc.notifyCallback != nil {
					if err := sc.notifyCallback(proxyID, secretName, newSecret); err != nil {
						log.Errorf("Failed to notify secret change for proxy %q: %v", proxyID, err)
					}
				} else {
					log.Warnf("secret cache notify callback isn't set")
				}
			}()
			// Currently only one ingress gateway is running, therefore there is at most one cache entry.
			// Stop the iteration once we have updated that cache entry.
			return false
		}
		return true
	})

	wg.Wait()
	for key, secret := range secretMap {
		sc.secrets.Store(key, *secret)
	}
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
	sc.rotationTicker = time.NewTicker(sc.configOptions.RotationInterval)
	for {
		select {
		case <-sc.rotationTicker.C:
			sc.rotate()
		case <-sc.closing:
			if sc.rotationTicker != nil {
				sc.rotationTicker.Stop()
			}
		}
	}
}

func (sc *SecretCache) rotate() {
	// Skip secret rotation for kubernetes secrets.
	if !sc.fetcher.UseCaClient {
		return
	}

	log.Debug("Refresh job running")

	secretMap := map[ConnKey]*model.SecretItem{}
	wg := sync.WaitGroup{}
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
		if now.After(e.CreatedTime.Add(sc.configOptions.EvictionDuration)) {
			sc.secrets.Delete(key)
			return true
		}

		// Re-generate secret if it's expired.
		if sc.shouldRefresh(&e) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if sc.isTokenExpired() {
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

				secretMap[key] = ns

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

	wg.Wait()

	for key, secret := range secretMap {
		sc.secrets.Store(key, *secret)
	}
}

func (sc *SecretCache) generateSecret(ctx context.Context, token, resourceName string, t time.Time) (*model.SecretItem, error) {
	// If node agent works as ingress gateway agent, searches for kubernetes secret instead of sending
	// CSR to CA.
	if sc.fetcher.UseCaClient == false {
		secretItem, exist := sc.fetcher.FindIngressGatewaySecret(resourceName)
		if !exist {
			return nil, fmt.Errorf("cannot find secret %s for ingress gateway", resourceName)
		}

		return &model.SecretItem{
			CertificateChain: secretItem.CertificateChain,
			PrivateKey:       secretItem.PrivateKey,
			ResourceName:     resourceName,
			Token:            token,
			CreatedTime:      t,
			Version:          t.String(),
		}, nil
	}

	// call authentication provider specific plugins to exchange token if necessary.
	exchangedToken := token
	var err error
	if sc.configOptions.Plugins != nil && len(sc.configOptions.Plugins) > 0 {
		for _, p := range sc.configOptions.Plugins {
			exchangedToken, _, err = p.ExchangeToken(ctx, sc.configOptions.TrustDomain, exchangedToken)
			if err != nil {
				log.Errorf("failed to exchange token: %v", err)
				return nil, err
			}
		}
	}

	// If token is jwt format, construct host name from jwt with format like spiffe://cluster.local/ns/foo/sa/sleep
	// otherwise just use sdsrequest.resourceName as csr host name.
	csrHostName, err := constructCSRHostName(sc.configOptions.TrustDomain, token)
	if err != nil {
		log.Warnf("failed to extract host name from jwt: %v, fallback to SDS request resource name", err)
		csrHostName = resourceName
	}
	options := util.CertOptions{
		Host:       csrHostName,
		RSAKeySize: keySize,
	}

	// Generate the cert/key, send CSR to CA.
	csrPEM, keyPEM, err := util.GenCSR(options)
	if err != nil {
		log.Errorf("Failed to generated key cert for %q: %v", resourceName, err)
		return nil, err
	}

	startTime := time.Now()
	retry := 0
	backOffInMilliSec := initialBackOffIntervalInMilliSec
	var certChainPEM []string
	for true {
		certChainPEM, err = sc.fetcher.CaClient.CSRSign(ctx, csrPEM, exchangedToken, int64(sc.configOptions.SecretTTL.Seconds()))
		if err == nil {
			break
		}

		// If non-retryable error, fail the request by returning err
		if !isRetryableErr(status.Code(err)) {
			log.Errorf("CSR for %q hit non-retryable error %v", resourceName, err)
			return nil, err
		}

		// If reach envoy timeout, fail the request by returning err
		if startTime.Add(time.Millisecond * envoyDefaultTimeoutInMilliSec).Before(time.Now()) {
			log.Errorf("CSR retry timeout for for %q: %v", resourceName, err)
			return nil, err
		}

		retry++
		backOffInMilliSec = retry * backOffInMilliSec
		randomTime := rand.Intn(initialBackOffIntervalInMilliSec)
		time.Sleep(time.Duration(backOffInMilliSec+randomTime) * time.Millisecond)
		log.Warnf("Failed to sign cert for %q: %v, will retry in %d millisec", resourceName, err, backOffInMilliSec)
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
		ResourceName:     resourceName,
		Token:            token,
		CreatedTime:      t,
		Version:          t.String(),
	}, nil
}

func (sc *SecretCache) shouldRefresh(s *model.SecretItem) bool {
	// secret should be refreshed before it expired, SecretRefreshGraceDuration is the grace period;
	// secret should be refreshed if time.Now.After(secret.CreateTime + SecretTTL - SecretRefreshGraceDuration)
	return time.Now().After(s.CreatedTime.Add(sc.configOptions.SecretTTL - sc.configOptions.SecretRefreshGraceDuration))
}

func (sc *SecretCache) isTokenExpired() bool {
	if atomic.LoadUint32(&sc.skipTokenExpireCheck) == 1 {
		return true
	}
	// TODO(quanlin), check if token has expired.
	return false
}

func constructCSRHostName(trustDomain, token string) (string, error) {
	// If token is jwt format, construct host name from jwt with format like spiffe://cluster.local/ns/foo/sa/sleep,
	strs := strings.Split(token, ".")
	if len(strs) != 3 {
		return "", fmt.Errorf("invalid k8s jwt token")
	}

	payload := strs[1]
	if l := len(payload) % 4; l > 0 {
		payload += strings.Repeat("=", 4-l)
	}
	dp, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return "", fmt.Errorf("invalid k8s jwt token: %v", err)
	}

	var jp k8sJwtPayload
	if err = json.Unmarshal(dp, &jp); err != nil {
		return "", fmt.Errorf("invalid k8s jwt token: %v", err)
	}

	// sub field in jwt should be in format like: system:serviceaccount:foo:bar
	ss := strings.Split(jp.Sub, ":")
	if len(ss) != 4 {
		return "", fmt.Errorf("invalid sub field in k8s jwt token")
	}
	ns := ss[2] //namespace
	sa := ss[3] //service account

	domain := "cluster.local"
	if trustDomain != "" {
		domain = trustDomain
	}

	return fmt.Sprintf(identityTemplate, domain, ns, sa), nil
}

func isRetryableErr(c codes.Code) bool {
	switch c {
	case codes.Canceled, codes.DeadlineExceeded, codes.ResourceExhausted, codes.Aborted, codes.Internal, codes.Unavailable:
		return true
	}
	return false
}
