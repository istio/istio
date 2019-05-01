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
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
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

	// The initial backoff time in millisecond to avoid the thundering herd problem.
	InitialBackoff int64

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

	// set this flag to true for if token used is always valid(ex, normal k8s JWT)
	AlwaysValidTokenFlag bool

	// set this flag to true if skip validate format for certificate chain returned from CA.
	SkipValidateCert bool
}

// SecretManager defines secrets management interface which is used by SDS.
type SecretManager interface {
	// GenerateSecret generates new secret and cache the secret.
	GenerateSecret(ctx context.Context, connectionID, resourceName, token string) (*model.SecretItem, error)

	// ShouldWaitForIngressGatewaySecret indicates whether a valid ingress gateway secret is expected.
	ShouldWaitForIngressGatewaySecret(connectionID, resourceName, token string) bool

	// SecretExist checks if secret already existed.
	// This API is used for sds server to check if coming request is ack request.
	SecretExist(connectionID, resourceName, token, version string) bool

	// DeleteSecret deletes a secret by its key from cache.
	DeleteSecret(connectionID, resourceName string)
}

// ConnKey is the key of one SDS connection.
type ConnKey struct {
	ConnectionID string

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

	// How may times that key rotation job has detected normal key/cert change happened, used in unit test.
	secretChangedCount uint64

	// How may times that key rotation job has detected root cert change happened, used in unit test.
	rootCertChangedCount uint64

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

	fetcher.AddCache = ret.UpdateK8sSecret
	fetcher.DeleteCache = ret.DeleteK8sSecret
	fetcher.UpdateCache = ret.UpdateK8sSecret

	atomic.StoreUint64(&ret.secretChangedCount, 0)
	atomic.StoreUint64(&ret.rootCertChangedCount, 0)
	atomic.StoreUint32(&ret.skipTokenExpireCheck, 1)
	go ret.keyCertRotationJob()
	return ret
}

// GenerateSecret generates new secret and cache the secret, this function is called by SDS.StreamSecrets
// and SDS.FetchSecret. Since credential passing from client may change, regenerate secret every time
// instead of reading from cache.
func (sc *SecretCache) GenerateSecret(ctx context.Context, connectionID, resourceName, token string) (*model.SecretItem, error) {
	var ns *model.SecretItem
	key := ConnKey{
		ConnectionID: connectionID,
		ResourceName: resourceName,
	}

	if resourceName != RootCertReqResourceName {
		// If working as Citadel agent, send request for normal key/cert pair.
		// If working as ingress gateway agent, fetch key/cert or root cert from SecretFetcher. Resource name for
		// root cert ends with "-cacert".
		ns, err := sc.generateSecret(ctx, token, resourceName, time.Now())
		if err != nil {
			log.Errorf("Failed to generate secret for proxy %q: %v", connectionID, err)
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
		log.Errorf("Failed to get root cert for proxy %q", connectionID)
		return nil, errors.New("failed to get root cert")

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
// This API is used for sds server to check if coming request is ack request.
func (sc *SecretCache) SecretExist(connectionID, resourceName, token, version string) bool {
	key := ConnKey{
		ConnectionID: connectionID,
		ResourceName: resourceName,
	}
	val, exist := sc.secrets.Load(key)
	if !exist {
		return false
	}

	e := val.(model.SecretItem)
	return e.ResourceName == resourceName && e.Token == token && e.Version == version
}

// IsIngressGatewaySecretReady returns true if node agent is working in ingress gateway agent mode
// and needs to wait for ingress gateway secret to be ready.
func (sc *SecretCache) ShouldWaitForIngressGatewaySecret(connectionID, resourceName, token string) bool {
	// If node agent works as workload agent, node agent does not expect any ingress gateway secret.
	if sc.fetcher.UseCaClient {
		return false
	}

	key := ConnKey{
		ConnectionID: connectionID,
		ResourceName: resourceName,
	}
	// Add an entry into cache, so that when ingress gateway secret is ready, gateway agent is able to
	// notify the ingress gateway and push the secret to via connect ID.
	if _, found := sc.secrets.Load(key); !found {
		t := time.Now()
		dummySecret := &model.SecretItem{
			ResourceName: resourceName,
			Token:        token,
			CreatedTime:  t,
			Version:      t.String(),
		}
		sc.secrets.Store(key, *dummySecret)
	}

	// If node agent works as ingress gateway agent, searches for kubernetes secret and verify secret
	// is not empty.
	secretItem, exist := sc.fetcher.FindIngressGatewaySecret(resourceName)
	// If kubernetes secret does not exist, need to wait for secret.
	if !exist {
		return true
	}

	// If expecting ingress gateway CA certificate, and that resource is empty, need to wait for
	// non empty resource.
	if strings.HasSuffix(resourceName, secretfetcher.IngressGatewaySdsCaSuffix) {
		return len(secretItem.RootCert) == 0
	}

	// If expect ingress gateway server certificate and private key, but at least one of them is
	// empty, need to wait for non empty resource.
	return len(secretItem.CertificateChain) == 0 || len(secretItem.PrivateKey) == 0
}

// DeleteSecret deletes a secret by its key from cache.
func (sc *SecretCache) DeleteSecret(connectionID, resourceName string) {
	key := ConnKey{
		ConnectionID: connectionID,
		ResourceName: resourceName,
	}
	sc.secrets.Delete(key)
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
			sc.rotate(false /*updateRootFlag*/)
		case <-sc.closing:
			if sc.rotationTicker != nil {
				sc.rotationTicker.Stop()
			}
		}
	}
}

// DeleteK8sSecret deletes all entries that match secretName. This is called when a K8s secret
// for ingress gateway is deleted.
func (sc *SecretCache) DeleteK8sSecret(secretName string) {
	wg := sync.WaitGroup{}
	sc.secrets.Range(func(k interface{}, v interface{}) bool {
		key := k.(ConnKey)

		if key.ResourceName == secretName {
			connectionID := key.ConnectionID
			sc.secrets.Delete(key)
			wg.Add(1)
			go func() {
				defer wg.Done()
				if sc.notifyCallback != nil {
					if err := sc.notifyCallback(connectionID, secretName, nil /*nil indicates close the streaming connection to proxy*/); err != nil {
						log.Errorf("Failed to notify secret change for proxy %q: %v", connectionID, err)
					}
				} else {
					log.Warn("secret cache notify callback isn't set")
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
	var secretMap sync.Map
	wg := sync.WaitGroup{}
	sc.secrets.Range(func(k interface{}, v interface{}) bool {
		key := k.(ConnKey)
		oldSecret := v.(model.SecretItem)
		if key.ResourceName == secretName {
			connectionID := key.ConnectionID
			wg.Add(1)
			go func() {
				defer wg.Done()

				var newSecret *model.SecretItem
				if strings.HasSuffix(secretName, secretfetcher.IngressGatewaySdsCaSuffix) {
					newSecret = &model.SecretItem{
						ResourceName: secretName,
						RootCert:     ns.RootCert,
						Token:        oldSecret.Token,
						CreatedTime:  ns.CreatedTime,
						Version:      ns.Version,
					}
				} else {
					newSecret = &model.SecretItem{
						CertificateChain: ns.CertificateChain,
						PrivateKey:       ns.PrivateKey,
						ResourceName:     secretName,
						Token:            oldSecret.Token,
						CreatedTime:      ns.CreatedTime,
						Version:          ns.Version,
					}
				}
				secretMap.Store(key, newSecret)
				if sc.notifyCallback != nil {
					if err := sc.notifyCallback(connectionID, secretName, newSecret); err != nil {
						log.Errorf("Failed to notify secret change for proxy %q: %v", connectionID, err)
					}
				} else {
					log.Warn("secret cache notify callback isn't set")
				}
			}()
			// Currently only one ingress gateway is running, therefore there is at most one cache entry.
			// Stop the iteration once we have updated that cache entry.
			return false
		}
		return true
	})

	wg.Wait()

	secretMap.Range(func(k interface{}, v interface{}) bool {
		key := k.(ConnKey)
		e := v.(*model.SecretItem)
		sc.secrets.Store(key, *e)
		return true
	})
}

func (sc *SecretCache) rotate(updateRootFlag bool) {
	// Skip secret rotation for kubernetes secrets.
	if !sc.fetcher.UseCaClient {
		return
	}

	log.Debug("Refresh job running")

	var secretMap sync.Map
	wg := sync.WaitGroup{}
	sc.secrets.Range(func(k interface{}, v interface{}) bool {
		key := k.(ConnKey)
		e := v.(model.SecretItem)
		connectionID := key.ConnectionID
		resourceName := key.ResourceName

		// only refresh root cert if updateRootFlag is set to true.
		if updateRootFlag {
			if key.ResourceName != RootCertReqResourceName {
				return true
			}

			atomic.AddUint64(&sc.rootCertChangedCount, 1)
			t := time.Now()
			ns := &model.SecretItem{
				ResourceName: resourceName,
				RootCert:     sc.rootCert,
				Token:        e.Token,
				CreatedTime:  t,
				Version:      t.String(),
			}
			secretMap.Store(key, ns)

			if sc.notifyCallback != nil {
				// Push the updated root cert to client.
				if err := sc.notifyCallback(connectionID, resourceName, ns); err != nil {
					log.Errorf("Failed to notify for proxy %q for resource %q: %v", connectionID, resourceName, err)
				}
			} else {
				log.Warn("secret cache notify callback isn't set")
			}

			return true
		}

		// If updateRootFlag isn't set, return directly if cached item is root cert.
		if key.ResourceName == RootCertReqResourceName {
			return true
		}

		now := time.Now()

		// Remove stale secrets from cache, this prevent the cache growing indefinitely.
		if now.After(e.CreatedTime.Add(sc.configOptions.EvictionDuration)) {
			sc.secrets.Delete(key)
			return true
		}

		// Re-generate secret if it's expired.
		if sc.shouldRefresh(&e) {
			atomic.AddUint64(&sc.secretChangedCount, 1)

			// Send the notification to close the stream if token is expired, so that client could re-connect with a new token.
			if sc.isTokenExpired() {
				log.Debugf("Token for %q expired for proxy %q", resourceName, connectionID)

				if sc.notifyCallback != nil {
					if err := sc.notifyCallback(connectionID, resourceName, nil /*nil indicates close the streaming connection to proxy*/); err != nil {
						log.Errorf("Failed to notify for proxy %q: %v", connectionID, err)
					}
				} else {
					log.Warn("secret cache notify callback isn't set")
				}

				return true
			}

			wg.Add(1)
			go func() {
				defer wg.Done()
				log.Debugf("Token for %q is still valid for proxy %q, use it to generate key/cert", resourceName, connectionID)

				// If token is still valid, re-generated the secret and push change to proxy.
				// Most likey this code path may not necessary, since TTL of cert is much longer than token.
				// When cert has expired, we could make it simple by assuming token has already expired.
				ns, err := sc.generateSecret(context.Background(), e.Token, resourceName, now)
				if err != nil {
					log.Errorf("Failed to generate secret for proxy %q: %v", connectionID, err)
					return
				}

				secretMap.Store(key, ns)

				if sc.notifyCallback != nil {
					if err := sc.notifyCallback(connectionID, resourceName, ns); err != nil {
						log.Errorf("Failed to notify secret change for proxy %q: %v", connectionID, err)
					}
				} else {
					log.Warn("secret cache notify callback isn't set")
				}

			}()
		}

		return true
	})

	wg.Wait()

	secretMap.Range(func(k interface{}, v interface{}) bool {
		key := k.(ConnKey)
		e := v.(*model.SecretItem)
		sc.secrets.Store(key, *e)
		return true
	})
}

func (sc *SecretCache) generateSecret(ctx context.Context, token, resourceName string, t time.Time) (*model.SecretItem, error) {
	// If node agent works as ingress gateway agent, searches for kubernetes secret instead of sending
	// CSR to CA.
	if !sc.fetcher.UseCaClient {
		secretItem, exist := sc.fetcher.FindIngressGatewaySecret(resourceName)
		if !exist {
			return nil, fmt.Errorf("cannot find secret %s for ingress gateway", resourceName)
		}

		if strings.HasSuffix(resourceName, secretfetcher.IngressGatewaySdsCaSuffix) {
			return &model.SecretItem{
				ResourceName: resourceName,
				RootCert:     secretItem.RootCert,
				Token:        token,
				CreatedTime:  t,
				Version:      t.String(),
			}, nil
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

	backOffInMilliSec := rand.Int63n(sc.configOptions.InitialBackoff)
	log.Debugf("Wait for %d millisec for initial CSR", backOffInMilliSec)
	// Add a jitter to initial CSR to avoid thundering herd problem.
	time.Sleep(time.Duration(backOffInMilliSec) * time.Millisecond)
	startTime := time.Now()
	var retry int64
	var certChainPEM []string
	for {
		certChainPEM, err = sc.fetcher.CaClient.CSRSign(
			ctx, csrPEM, exchangedToken, int64(sc.configOptions.SecretTTL.Seconds()))
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
			log.Errorf("CSR retry timeout for %q: %v", resourceName, err)
			return nil, err
		}

		retry++
		backOffInMilliSec = rand.Int63n(retry * initialBackOffIntervalInMilliSec)
		time.Sleep(time.Duration(backOffInMilliSec) * time.Millisecond)
		log.Warnf("CSR failed for %q: %v, retry in %d millisec", resourceName, err, backOffInMilliSec)
	}

	log.Debugf("CSR response certificate chain %+v \n", certChainPEM)

	certChain := []byte{}
	for _, c := range certChainPEM {
		certChain = append(certChain, []byte(c)...)
	}

	// Cert exipre time by default is createTime + sc.configOptions.SecretTTL.
	// Citadel respects SecretTTL that passed to it and use it decide TTL of cert it issued.
	// Some customer CA may override TTL param that's passed to it.
	expireTime := t.Add(sc.configOptions.SecretTTL)
	if !sc.configOptions.SkipValidateCert {
		block, _ := pem.Decode(certChain)
		if block == nil {
			log.Errorf("Failed to decode certificate %+v for %q", certChainPEM, resourceName)
			return nil, errors.New("failed to decode certificate")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			log.Errorf("Failed to parse certificate %+v for %q: %v", certChainPEM, resourceName, err)
			return nil, errors.New("failed to parse certificate")
		}
		expireTime = cert.NotAfter
	}

	length := len(certChainPEM)
	// Leaf cert is element '0'. Root cert is element 'n'.
	rootCertChanged := !bytes.Equal(sc.rootCert, []byte(certChainPEM[length-1]))
	if sc.rootCert == nil || rootCertChanged {
		sc.rootCertMutex.Lock()
		sc.rootCert = []byte(certChainPEM[length-1])
		sc.rootCertMutex.Unlock()
	}

	if rootCertChanged {
		log.Info("Root cert has changed")
		sc.rotate(true /*updateRootFlag*/)
	}

	return &model.SecretItem{
		CertificateChain: certChain,
		PrivateKey:       keyPEM,
		ResourceName:     resourceName,
		Token:            token,
		CreatedTime:      t,
		ExpireTime:       expireTime,
		Version:          t.String(),
	}, nil
}

func (sc *SecretCache) shouldRefresh(s *model.SecretItem) bool {
	// secret should be refreshed before it expired, SecretRefreshGraceDuration is the grace period;
	return time.Now().After(s.ExpireTime.Add(-sc.configOptions.SecretRefreshGraceDuration))
}

func (sc *SecretCache) isTokenExpired() bool {
	// skip check if the token passed from envoy is always valid (ex, normal k8s sa JWT).
	if sc.configOptions.AlwaysValidTokenFlag {
		return false
	}

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
