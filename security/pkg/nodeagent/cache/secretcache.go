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

// Package cache is the in-memory secret store.
package cache

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"istio.io/istio/pkg/security"
	"istio.io/pkg/filewatcher"
	"istio.io/pkg/log"

	pilotmodel "istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/mcp/status"
	"istio.io/istio/security/pkg/nodeagent/secretfetcher"
	nodeagentutil "istio.io/istio/security/pkg/nodeagent/util"
	pkiutil "istio.io/istio/security/pkg/pki/util"
	"istio.io/istio/security/pkg/util"

	"github.com/google/uuid"
)

var (
	cacheLog       = log.RegisterScope("cache", "cache debugging", 0)
	newFileWatcher = filewatcher.NewWatcher
	// The total timeout for any credential retrieval process, default value of 10s is used.
	totalTimeout = time.Second * 10
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

	// WorkloadKeyCertResourceName is the resource name of the discovery request for workload
	// identity.
	// TODO: change all the pilot one reference definition here instead.
	WorkloadKeyCertResourceName = "default"

	// identityTemplate is the format template of identity in the CSR request.
	identityTemplate = "spiffe://%s/ns/%s/sa/%s"

	// firstRetryBackOffInMilliSec is the initial backoff time interval when hitting
	// non-retryable error in CSR request or while there is an error in reading file mounts.
	firstRetryBackOffInMilliSec = 50

	// notifySecretRetrievalTimeout is the timeout for another round of secret retrieval. This is to make sure to
	// unblock the secret watch main thread in case those child threads got stuck due to any reason.
	notifySecretRetrievalTimeout = 30 * time.Second
)

type k8sJwtPayload struct {
	Sub string `json:"sub"`
}

// SecretManager defines secrets management interface which is used by SDS.
type SecretManager interface {
	// GenerateSecret generates new secret and cache the secret.
	// Current implementation constructs the SAN based on the token's 'sub'
	// claim, expected to be in the K8S format. No other JWTs are currently supported
	// due to client logic. If JWT is missing/invalid, the resourceName is used.
	GenerateSecret(ctx context.Context, connectionID, resourceName, token string) (*security.SecretItem, error)

	// ShouldWaitForGatewaySecret indicates whether a valid gateway secret is expected.
	ShouldWaitForGatewaySecret(connectionID, resourceName, token string, fileMountedCertsOnly bool) bool

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
	configOptions *security.Options

	// How may times that key rotation job has detected normal key/cert change happened, used in unit test.
	secretChangedCount uint64

	// How may times that key rotation job has detected root cert change happened, used in unit test.
	rootCertChangedCount uint64

	// callback function to invoke when detecting secret change.
	notifyCallback func(connKey ConnKey, secret *security.SecretItem) error

	// close channel.
	closing chan bool

	// Read/Write to rootCert and rootCertExpireTime should use getRootCert() and setRootCert().
	rootCertMutex      *sync.RWMutex
	rootCert           []byte
	rootCertExpireTime time.Time

	// Source of random numbers. It is not concurrency safe, requires lock protected.
	rand      *rand.Rand
	randMutex *sync.Mutex

	// The paths for an existing certificate chain, key and root cert files. Istio agent will
	// use them as the source of secrets if they exist.
	existingCertChainFile string
	existingKeyFile       string
	existingRootCertFile  string

	// certWatcher watches the certificates for changes and triggers a notification to proxy.
	certWatcher filewatcher.FileWatcher
	// unique certs being watched with file watcher.
	fileCerts map[string]map[ConnKey]struct{}
	certMutex *sync.RWMutex
}

// NewSecretCache creates a new secret cache.
func NewSecretCache(fetcher *secretfetcher.SecretFetcher,
	notifyCb func(ConnKey, *security.SecretItem) error, options *security.Options) *SecretCache {
	ret := &SecretCache{
		fetcher:               fetcher,
		closing:               make(chan bool),
		notifyCallback:        notifyCb,
		rootCertMutex:         &sync.RWMutex{},
		configOptions:         options,
		randMutex:             &sync.Mutex{},
		existingCertChainFile: security.DefaultCertChainFilePath,
		existingKeyFile:       security.DefaultKeyFilePath,
		existingRootCertFile:  security.DefaultRootCertFilePath,
		certWatcher:           newFileWatcher(),
		fileCerts:             make(map[string]map[ConnKey]struct{}),
		certMutex:             &sync.RWMutex{},
	}
	randSource := rand.NewSource(time.Now().UnixNano())
	ret.rand = rand.New(randSource)

	fetcher.AddCache = ret.UpdateK8sSecret
	fetcher.DeleteCache = ret.DeleteK8sSecret
	fetcher.UpdateCache = ret.UpdateK8sSecret

	atomic.StoreUint64(&ret.secretChangedCount, 0)
	atomic.StoreUint64(&ret.rootCertChangedCount, 0)
	go ret.keyCertRotationJob()
	return ret
}

// getRootCertInfo returns cached root cert and cert expiration time. This method is thread safe.
func (sc *SecretCache) getRootCert() (rootCert []byte, rootCertExpr time.Time) {
	sc.rootCertMutex.RLock()
	rootCert = sc.rootCert
	rootCertExpr = sc.rootCertExpireTime
	sc.rootCertMutex.RUnlock()
	return rootCert, rootCertExpr
}

// setRootCert sets root cert into cache. This method is thread safe.
func (sc *SecretCache) setRootCert(rootCert []byte, rootCertExpr time.Time) {
	sc.rootCertMutex.Lock()
	sc.rootCert = rootCert
	sc.rootCertExpireTime = rootCertExpr
	sc.rootCertMutex.Unlock()
}

// GenerateSecret generates new secret and cache the secret, this function is called by SDS.StreamSecrets
// and SDS.FetchSecret. Since credential passing from client may change, regenerate secret every time
// instead of reading from cache.
func (sc *SecretCache) GenerateSecret(ctx context.Context, connectionID, resourceName, token string) (*security.SecretItem, error) {
	connKey := ConnKey{
		ConnectionID: connectionID,
		ResourceName: resourceName,
	}

	logPrefix := cacheLogPrefix(resourceName)

	// When there are existing root certificates, or private key and certificate under
	// a well known path, they are used in the SDS response.
	var err error
	var ns *security.SecretItem

	// First try to generate secret from file.
	sdsFromFile, ns, err := sc.generateFileSecret(connKey, token)

	if sdsFromFile {
		if err != nil {
			return nil, err
		}
		return ns, nil
	}

	if resourceName != RootCertReqResourceName {
		// If working as Citadel agent, send request for normal key/cert pair.
		// If working as ingress gateway agent, fetch key/cert or root cert from SecretFetcher. Resource name for
		// root cert ends with "-cacert".
		ns, err := sc.generateSecret(ctx, token, connKey, time.Now())
		if err != nil {
			cacheLog.Errorf("%s failed to generate secret for proxy: %v",
				logPrefix, err)
			return nil, err
		}

		cacheLog.Infoa("GenerateSecret ", resourceName)
		sc.secrets.Store(connKey, *ns)
		return ns, nil
	}

	// If request is for root certificate,
	// retry since rootCert may be empty until there is CSR response returned from CA.
	rootCert, rootCertExpr := sc.getRootCert()
	if rootCert == nil {
		wait := retryWaitDuration
		retryNum := 0
		for ; retryNum < maxRetryNum; retryNum++ {
			time.Sleep(wait)
			rootCert, rootCertExpr = sc.getRootCert()
			if rootCert != nil {
				break
			}

			wait *= 2
		}
	}

	if rootCert == nil {
		cacheLog.Errorf("%s failed to get root cert for proxy", logPrefix)
		return nil, errors.New("failed to get root cert")
	}

	t := time.Now()
	ns = &security.SecretItem{
		ResourceName: resourceName,
		RootCert:     rootCert,
		ExpireTime:   rootCertExpr,
		Token:        token,
		CreatedTime:  t,
		Version:      t.String(),
	}
	cacheLog.Infoa("Loaded root cert from certificate ", resourceName)
	sc.secrets.Store(connKey, *ns)
	cacheLog.Debugf("%s successfully generate secret for proxy", logPrefix)
	return ns, nil
}

func (sc *SecretCache) addFileWatcher(file string, token string, connKey ConnKey) {
	// TODO(ramaraochavali): add integration test for file watcher functionality.
	// Check if this file is being already watched, if so ignore it. FileWatcher has the functionality of
	// checking for duplicates. This check is needed here to avoid processing duplicate events for the same file.
	sc.certMutex.Lock()
	npath := filepath.Clean(file)
	watching := false
	if _, watching = sc.fileCerts[npath]; !watching {
		sc.fileCerts[npath] = make(map[ConnKey]struct{})
	}
	// Add connKey to the path - so that whenever it changes, connection is also pushed.
	sc.fileCerts[npath][connKey] = struct{}{}
	sc.certMutex.Unlock()
	if watching {
		return
	}
	// File is not being watched, start watching now and trigger key push.
	cacheLog.Infof("adding watcher for file %s", file)
	if err := sc.certWatcher.Add(file); err != nil {
		cacheLog.Errorf("%v: error adding watcher for file, skipping watches [%s] %v", connKey, file, err)
		return
	}
	go func() {
		var timerC <-chan time.Time
		for {
			select {
			case <-timerC:
				timerC = nil
				// TODO(ramaraochavali): Remove the watchers for unused keys and certs.
				sc.certMutex.RLock()
				connKeys := sc.fileCerts[npath]
				sc.certMutex.RUnlock()
				// Update all connections that use this file.
				for ckey := range connKeys {
					if _, ok := sc.secrets.Load(ckey); ok {
						// Regenerate the Secret and trigger the callback that pushes the secrets to proxy.
						if _, secret, err := sc.generateFileSecret(ckey, token); err != nil {
							cacheLog.Errorf("%v: error in generating secret after file change [%s] %v", ckey, file, err)
						} else {
							cacheLog.Infof("%v: file changed, triggering secret push to proxy [%s]", ckey, file)
							sc.callbackWithTimeout(ckey, secret)
						}
					}
				}
			case e := <-sc.certWatcher.Events(file):
				if len(e.Op.String()) > 0 { // To avoid spurious events, mainly coming from tests.
					// Use a timer to debounce watch updates
					if timerC == nil {
						timerC = time.After(100 * time.Millisecond) // TODO: Make this configurable if needed.
					}
				}
			}
		}
	}()
}

// SecretExist checks if secret already existed.
// This API is used for sds server to check if coming request is ack request.
func (sc *SecretCache) SecretExist(connectionID, resourceName, token, version string) bool {
	connKey := ConnKey{
		ConnectionID: connectionID,
		ResourceName: resourceName,
	}
	val, exist := sc.secrets.Load(connKey)
	if !exist {
		return false
	}

	secret := val.(security.SecretItem)
	return secret.ResourceName == resourceName && secret.Token == token && secret.Version == version
}

// ShouldWaitForGatewaySecret returns true if node agent is working in gateway agent mode
// and needs to wait for gateway secret to be ready.
func (sc *SecretCache) ShouldWaitForGatewaySecret(connectionID, resourceName, token string, fileMountedCertsOnly bool) bool {
	// If node agent works as workload agent, node agent does not expect any gateway secret.
	// If workload is using file mounted certs, we should not wait for ingress secret.
	if sc.fetcher.UseCaClient || fileMountedCertsOnly {
		return false
	}

	connKey := ConnKey{
		ConnectionID: connectionID,
		ResourceName: resourceName,
	}
	// Add an entry into cache, so that when gateway secret is ready, gateway agent is able to
	// notify the gateway and push the secret to via connect ID.
	if _, found := sc.secrets.Load(connKey); !found {
		t := time.Now()
		dummySecret := &security.SecretItem{
			ResourceName: resourceName,
			Token:        token,
			CreatedTime:  t,
			Version:      t.String(),
		}
		sc.secrets.Store(connKey, *dummySecret)
	}

	logPrefix := cacheLogPrefix(resourceName)
	// If node agent works as gateway agent, searches for kubernetes secret and verify secret
	// is not empty.
	cacheLog.Debugf("%s calling SecretFetcher to search for secret %s",
		logPrefix, resourceName)
	_, exist := sc.fetcher.FindGatewaySecret(resourceName)
	// If kubernetes secret does not exist, need to wait for secret.
	if !exist {
		cacheLog.Warnf("%s SecretFetcher cannot find secret %s from cache",
			logPrefix, resourceName)
		return true
	}

	return false
}

// DeleteSecret deletes a secret by its key from cache.
func (sc *SecretCache) DeleteSecret(connectionID, resourceName string) {
	connKey := ConnKey{
		ConnectionID: connectionID,
		ResourceName: resourceName,
	}
	sc.secrets.Delete(connKey)
}

func (sc *SecretCache) callbackWithTimeout(connKey ConnKey, secret *security.SecretItem) {
	c := make(chan struct{})
	logPrefix := cacheLogPrefix(connKey.ResourceName)
	go func() {
		defer close(c)
		if sc.notifyCallback == nil {
			cacheLog.Warnf("%s secret cache notify callback isn't set", logPrefix)
			return
		}
		if err := sc.notifyCallback(connKey, secret); err != nil {
			cacheLog.Errorf("%s failed to notify secret change for proxy: %v",
				logPrefix, err)
		}
	}()
	select {
	case <-c:
		return // completed normally
	case <-time.After(notifySecretRetrievalTimeout):
		cacheLog.Warnf("%s notify secret change for proxy got timeout", logPrefix)
	}
}

// Close shuts down the secret cache.
func (sc *SecretCache) Close() {
	_ = sc.certWatcher.Close()
	sc.closing <- true
}

func (sc *SecretCache) keyCertRotationJob() {
	// Wake up once in a while and rotate keys and certificates if in grace period.
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
// for gateway is deleted.
func (sc *SecretCache) DeleteK8sSecret(secretName string) {
	wg := sync.WaitGroup{}
	sc.secrets.Range(func(k interface{}, v interface{}) bool {
		connKey := k.(ConnKey)
		if connKey.ResourceName == secretName {
			sc.secrets.Delete(connKey)
			cacheLog.Debugf("%s secret cache is deleted", cacheLogPrefix(secretName))
			wg.Add(1)
			go func() {
				defer wg.Done()
				sc.callbackWithTimeout(connKey, nil /*nil indicates close the streaming connection to proxy*/)
			}()
			// Currently only one gateway is running, therefore there is at most one cache entry.
			// Stop the iteration once we have deleted that cache entry.
			return false
		}
		return true
	})
	wg.Wait()
}

// UpdateK8sSecret updates all entries that match secretName. This is called when a K8s secret
// for gateway is updated.
func (sc *SecretCache) UpdateK8sSecret(secretName string, ns security.SecretItem) {
	var secretMap sync.Map
	wg := sync.WaitGroup{}
	sc.secrets.Range(func(k interface{}, v interface{}) bool {
		connKey := k.(ConnKey)
		oldSecret := v.(security.SecretItem)
		if connKey.ResourceName == secretName {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var newSecret *security.SecretItem
				if strings.HasSuffix(secretName, secretfetcher.GatewaySdsCaSuffix) {
					newSecret = &security.SecretItem{
						ResourceName: secretName,
						RootCert:     ns.RootCert,
						ExpireTime:   ns.ExpireTime,
						Token:        oldSecret.Token,
						CreatedTime:  ns.CreatedTime,
						Version:      ns.Version,
					}
				} else {
					newSecret = &security.SecretItem{
						CertificateChain: ns.CertificateChain,
						ExpireTime:       ns.ExpireTime,
						PrivateKey:       ns.PrivateKey,
						ResourceName:     secretName,
						Token:            oldSecret.Token,
						CreatedTime:      ns.CreatedTime,
						Version:          ns.Version,
					}
				}
				secretMap.Store(connKey, newSecret)
				cacheLog.Debugf("%s secret cache is updated", cacheLogPrefix(secretName))
				sc.callbackWithTimeout(connKey, newSecret)
			}()
			// Currently only one gateway is running, therefore there is at most one cache entry.
			// Stop the iteration once we have updated that cache entry.
			return false
		}
		return true
	})

	wg.Wait()

	secretMap.Range(func(k interface{}, v interface{}) bool {
		key := k.(ConnKey)
		secret := v.(*security.SecretItem)
		sc.secrets.Store(key, *secret)
		return true
	})
}

func (sc *SecretCache) rotate(updateRootFlag bool) {
	// Skip secret rotation for kubernetes secrets.
	if !sc.fetcher.UseCaClient {
		return
	}

	cacheLog.Debug("Rotation job running")

	var secretMap sync.Map
	wg := sync.WaitGroup{}
	sc.secrets.Range(func(k interface{}, v interface{}) bool {
		connKey := k.(ConnKey)
		secret := v.(security.SecretItem)
		logPrefix := cacheLogPrefix(connKey.ResourceName)

		// only rotate root cert if updateRootFlag is set to true.
		if updateRootFlag {
			if connKey.ResourceName != RootCertReqResourceName {
				return true
			}

			atomic.AddUint64(&sc.rootCertChangedCount, 1)
			now := time.Now()
			rootCert, rootCertExpr := sc.getRootCert()
			ns := &security.SecretItem{
				ResourceName: connKey.ResourceName,
				RootCert:     rootCert,
				ExpireTime:   rootCertExpr,
				Token:        secret.Token,
				CreatedTime:  now,
				Version:      now.String(),
			}
			secretMap.Store(connKey, ns)
			cacheLog.Debugf("%s secret cache is updated", logPrefix)
			sc.callbackWithTimeout(connKey, ns)

			return true
		}

		// If updateRootFlag isn't set, return directly if cached item is root cert.
		if connKey.ResourceName == RootCertReqResourceName {
			return true
		}

		now := time.Now()

		// Remove stale secrets from cache, this prevents the cache growing indefinitely.
		if sc.configOptions.EvictionDuration != 0 && now.After(secret.CreatedTime.Add(sc.configOptions.EvictionDuration)) {
			sc.secrets.Delete(connKey)
			return true
		}

		if sc.configOptions.CredFetcher != nil {
			// Refresh token through credential fetcher.
			cacheLog.Debugf("%s getting a new token through credential fetcher", logPrefix)
			t, err := sc.configOptions.CredFetcher.GetPlatformCredential()
			if err != nil {
				cacheLog.Warnf("%s credential fetcher failed to get a new token, continue using the original token: %v", logPrefix, err)
			} else {
				secret.Token = t
			}
		}

		// Re-generate secret if it's expired.
		if sc.shouldRotate(&secret) {
			atomic.AddUint64(&sc.secretChangedCount, 1)
			// Send the notification to close the stream if token is expired, so that client could re-connect with a new token.
			if sc.isTokenExpired(&secret) && !sc.useCertToRotate() {
				cacheLog.Debugf("%s token expired", logPrefix)
				// TODO(myidpt): Optimization needed. When using local JWT, server should directly push the new secret instead of
				// requiring the client to send another SDS request.
				sc.callbackWithTimeout(connKey, nil /*nil indicates close the streaming connection to proxy*/)
				return true
			}

			wg.Add(1)
			go func() {
				defer wg.Done()
				cacheLog.Debugf("%s use token to generate key/cert", logPrefix)

				// If token is still valid, re-generated the secret and push change to proxy.
				// Most likely this code path may not necessary, since TTL of cert is much longer than token.
				// When cert has expired, we could make it simple by assuming token has already expired.
				ns, err := sc.generateSecret(context.Background(), secret.Token, connKey, now)
				if err != nil {
					cacheLog.Errorf("%s failed to rotate secret: %v", logPrefix, err)
					return
				}
				// Output the key and cert to dir to make sure key and cert are rotated.
				if err = nodeagentutil.OutputKeyCertToDir(sc.configOptions.OutputKeyCertToDir, ns.PrivateKey,
					ns.CertificateChain, ns.RootCert); err != nil {
					cacheLog.Errorf("(%v) error when output the key and cert: %v",
						connKey, err)
					return
				}

				secretMap.Store(connKey, ns)
				cacheLog.Debugf("%s secret cache is updated", logPrefix)
				sc.callbackWithTimeout(connKey, ns)

			}()
		}

		return true
	})

	wg.Wait()

	secretMap.Range(func(k interface{}, v interface{}) bool {
		key := k.(ConnKey)
		secret := v.(*security.SecretItem)
		sc.secrets.Store(key, *secret)
		return true
	})
}

// generateGatewaySecret returns secret for gateway proxy.
func (sc *SecretCache) generateGatewaySecret(token string, connKey ConnKey, now time.Time) (*security.SecretItem, error) {
	secretItem, exist := sc.fetcher.FindGatewaySecret(connKey.ResourceName)
	if !exist {
		return nil, fmt.Errorf("cannot find secret for gateway SDS request %+v", connKey)
	}

	if strings.HasSuffix(connKey.ResourceName, secretfetcher.GatewaySdsCaSuffix) {
		return &security.SecretItem{
			ResourceName: connKey.ResourceName,
			RootCert:     secretItem.RootCert,
			ExpireTime:   secretItem.ExpireTime,
			Token:        token,
			CreatedTime:  now,
			Version:      now.String(),
		}, nil
	}
	return &security.SecretItem{
		CertificateChain: secretItem.CertificateChain,
		ExpireTime:       secretItem.ExpireTime,
		PrivateKey:       secretItem.PrivateKey,
		ResourceName:     connKey.ResourceName,
		Token:            token,
		CreatedTime:      now,
		Version:          now.String(),
	}, nil
}

// If there is existing root certificates under a well known path, return true.
// Otherwise, return false.
func (sc *SecretCache) rootCertificateExist(filePath string) bool {
	b, err := ioutil.ReadFile(filePath)
	if err != nil || len(b) == 0 {
		return false
	}
	return true
}

// If there is an existing private key and certificate under a well known path, return true.
// Otherwise, return false.
func (sc *SecretCache) keyCertificateExist(certPath, keyPath string) bool {
	b, err := ioutil.ReadFile(certPath)
	if err != nil || len(b) == 0 {
		return false
	}
	b, err = ioutil.ReadFile(keyPath)
	if err != nil || len(b) == 0 {
		return false
	}

	return true
}

// Generate a root certificate item from the passed in rootCertPath
func (sc *SecretCache) generateRootCertFromExistingFile(rootCertPath, token string, connKey ConnKey) (*security.SecretItem, error) {
	rootCert, err := readFileWithTimeout(rootCertPath)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	var certExpireTime time.Time
	if certExpireTime, err = nodeagentutil.ParseCertAndGetExpiryTimestamp(rootCert); err != nil {
		cacheLog.Errorf("failed to extract expiration time in the root certificate loaded from file: %v", err)
		return nil, fmt.Errorf("failed to extract expiration time in the root certificate loaded from file: %v", err)
	}

	// Set the rootCert
	sc.setRootCert(rootCert, certExpireTime)
	return &security.SecretItem{
		ResourceName: connKey.ResourceName,
		RootCert:     rootCert,
		ExpireTime:   certExpireTime,
		Token:        token,
		CreatedTime:  now,
		Version:      now.String(),
	}, nil
}

// Generate a key and certificate item from the existing key certificate files from the passed in file paths.
func (sc *SecretCache) generateKeyCertFromExistingFiles(certChainPath, keyPath, token string, connKey ConnKey) (*security.SecretItem, error) {
	certChain, err := readFileWithTimeout(certChainPath)
	if err != nil {
		return nil, err
	}
	keyPEM, err := readFileWithTimeout(keyPath)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	var certExpireTime time.Time
	if certExpireTime, err = nodeagentutil.ParseCertAndGetExpiryTimestamp(certChain); err != nil {
		cacheLog.Errorf("failed to extract expiration time in the certificate loaded from file: %v", err)
		return nil, fmt.Errorf("failed to extract expiration time in the certificate loaded from file: %v", err)
	}

	return &security.SecretItem{
		CertificateChain: certChain,
		PrivateKey:       keyPEM,
		ResourceName:     connKey.ResourceName,
		Token:            token,
		CreatedTime:      now,
		ExpireTime:       certExpireTime,
		Version:          now.String(),
	}, nil
}

// readFileWithTimeout reads the given file with timeout. It returns error
// if it is not able to read file after timeout.
func readFileWithTimeout(path string) ([]byte, error) {
	retryBackoffInMS := int64(firstRetryBackOffInMilliSec)
	for {
		cert, err := ioutil.ReadFile(path)
		if err == nil {
			return cert, nil
		}
		select {
		case <-time.After(time.Duration(retryBackoffInMS)):
			retryBackoffInMS *= 2
		case <-time.After(totalTimeout):
			return nil, err
		}
	}
}

func (sc *SecretCache) generateFileSecret(connKey ConnKey, token string) (bool, *security.SecretItem, error) {
	resourceName := connKey.ResourceName
	logPrefix := cacheLogPrefix(resourceName)

	// When there are existing root certificates, or private key and certificate under
	// a well known path, they are used in the SDS response.
	sdsFromFile := false
	var err error
	var sitem *security.SecretItem

	switch {
	// Default root certificate.
	case connKey.ResourceName == RootCertReqResourceName && sc.rootCertificateExist(sc.existingRootCertFile):
		sdsFromFile = true
		if sitem, err = sc.generateRootCertFromExistingFile(sc.existingRootCertFile, token, connKey); err == nil {
			sc.addFileWatcher(sc.existingRootCertFile, token, connKey)
		}
	// Default workload certificate.
	case connKey.ResourceName == WorkloadKeyCertResourceName && sc.keyCertificateExist(sc.existingCertChainFile, sc.existingKeyFile):
		sdsFromFile = true
		if sitem, err = sc.generateKeyCertFromExistingFiles(sc.existingCertChainFile, sc.existingKeyFile, token, connKey); err == nil {
			// Adding cert is sufficient here as key can't change without changing the cert.
			sc.addFileWatcher(sc.existingCertChainFile, token, connKey)
		}
	default:
		// Check if the resource name refers to a file mounted certificate.
		// Currently used in destination rules and server certs (via metadata).
		// Based on the resource name, we need to read the secret from a file encoded in the resource name.
		cfg, ok := pilotmodel.SdsCertificateConfigFromResourceName(connKey.ResourceName)
		sdsFromFile = ok
		switch {
		case ok && cfg.IsRootCertificate():
			if sitem, err = sc.generateRootCertFromExistingFile(cfg.CaCertificatePath, token, connKey); err == nil {
				sc.addFileWatcher(cfg.CaCertificatePath, token, connKey)
			}
		case ok && cfg.IsKeyCertificate():
			if sitem, err = sc.generateKeyCertFromExistingFiles(cfg.CertificatePath, cfg.PrivateKeyPath, token, connKey); err == nil {
				// Adding cert is sufficient here as key can't change without changing the cert.
				sc.addFileWatcher(cfg.CertificatePath, token, connKey)
			}
		}
	}

	if sdsFromFile {
		if err != nil {
			cacheLog.Errorf("%s failed to generate secret for proxy from file: %v",
				logPrefix, err)
			return sdsFromFile, nil, err
		}
		cacheLog.Infoa("GenerateSecret from file ", resourceName)
		sc.secrets.Store(connKey, *sitem)
		return sdsFromFile, sitem, nil
	}
	return sdsFromFile, nil, nil
}

func (sc *SecretCache) generateSecret(ctx context.Context, token string, connKey ConnKey, t time.Time) (*security.SecretItem, error) {
	// If node agent works as gateway agent, searches for kubernetes secret instead of sending
	// CSR to CA.
	if !sc.fetcher.UseCaClient {
		return sc.generateGatewaySecret(token, connKey, t)
	}

	logPrefix := cacheLogPrefix(connKey.ResourceName)
	// call authentication provider specific plugins to exchange token if necessary.
	numOutgoingRequests.With(RequestType.Value(TokenExchange)).Increment()
	timeBeforeTokenExchange := time.Now()
	exchangedToken, err := sc.getExchangedToken(ctx, token, connKey)
	tokenExchangeLatency := float64(time.Since(timeBeforeTokenExchange).Nanoseconds()) / float64(time.Millisecond)
	outgoingLatency.With(RequestType.Value(TokenExchange)).Record(tokenExchangeLatency)
	if err != nil {
		numFailedOutgoingRequests.With(RequestType.Value(TokenExchange)).Increment()
		return nil, err
	}

	// If token is jwt format, construct host name from jwt with format like spiffe://cluster.local/ns/foo/sa/sleep
	// otherwise just use sdsrequest.resourceName as csr host name.
	csrHostName := connKey.ResourceName
	// TODO (liminw): This is probably not needed. CA is using claims in the credential to decide the identity in the certificate,
	// instead of using host name in CSR. We can clean it up later.
	if sc.configOptions.CredFetcher == nil {
		csrHostName, err = constructCSRHostName(sc.configOptions.TrustDomain, token)
		if err != nil {
			cacheLog.Warnf("%s failed to extract host name from jwt: %v, fallback to SDS request"+
				" resource name: %s", logPrefix, err, connKey.ResourceName)
			csrHostName = connKey.ResourceName
		}
	}
	cacheLog.Debugf("constructed host name for CSR: %s", csrHostName)
	options := pkiutil.CertOptions{
		Host:       csrHostName,
		RSAKeySize: keySize,
		PKCS8Key:   sc.configOptions.Pkcs8Keys,
		ECSigAlg:   pkiutil.SupportedECSignatureAlgorithms(sc.configOptions.ECCSigAlg),
	}

	// Generate the cert/key, send CSR to CA.
	csrPEM, keyPEM, err := pkiutil.GenCSR(options)
	if err != nil {
		cacheLog.Errorf("%s failed to generate key and certificate for CSR: %v", logPrefix, err)
		return nil, err
	}

	numOutgoingRequests.With(RequestType.Value(CSR)).Increment()
	timeBeforeCSR := time.Now()
	certChainPEM, err := sc.sendRetriableRequest(ctx, csrPEM, exchangedToken, connKey, true)
	csrLatency := float64(time.Since(timeBeforeCSR).Nanoseconds()) / float64(time.Millisecond)
	outgoingLatency.With(RequestType.Value(CSR)).Record(csrLatency)
	if err != nil {
		numFailedOutgoingRequests.With(RequestType.Value(CSR)).Increment()
		return nil, err
	}

	cacheLog.Debugf("%s received CSR response with certificate chain %+v \n",
		logPrefix, certChainPEM)

	certChain := []byte{}
	for _, c := range certChainPEM {
		certChain = append(certChain, []byte(c)...)
	}

	var expireTime time.Time
	// Cert expire time by default is createTime + sc.configOptions.SecretTTL.
	// Istiod respects SecretTTL that passed to it and use it decide TTL of cert it issued.
	// Some customer CA may override TTL param that's passed to it.
	if expireTime, err = nodeagentutil.ParseCertAndGetExpiryTimestamp(certChain); err != nil {
		cacheLog.Errorf("%s failed to extract expire time from server certificate in CSR response %+v: %v",
			logPrefix, certChainPEM, err)
		return nil, fmt.Errorf("failed to extract expire time from server certificate in CSR response: %v", err)
	}

	length := len(certChainPEM)
	rootCert, _ := sc.getRootCert()
	// Leaf cert is element '0'. Root cert is element 'n'.
	rootCertChanged := !bytes.Equal(rootCert, []byte(certChainPEM[length-1]))
	if rootCert == nil || rootCertChanged {
		rootCertExpireTime, err := nodeagentutil.ParseCertAndGetExpiryTimestamp([]byte(certChainPEM[length-1]))
		if err == nil {
			sc.setRootCert([]byte(certChainPEM[length-1]), rootCertExpireTime)
		} else {
			cacheLog.Errorf("%s failed to parse root certificate in CSR response: %v", logPrefix, err)
			rootCertChanged = false
		}
	}

	if rootCertChanged {
		cacheLog.Info("Root cert has changed, start rotating root cert for SDS clients")
		sc.rotate(true /*updateRootFlag*/)
	}

	return &security.SecretItem{
		CertificateChain: certChain,
		PrivateKey:       keyPEM,
		ResourceName:     connKey.ResourceName,
		Token:            token,
		CreatedTime:      t,
		ExpireTime:       expireTime,
		Version:          t.Format("01-02 15:04:05.000"), // Precise enough version based on creation time.
	}, nil
}

func (sc *SecretCache) shouldRotate(secret *security.SecretItem) bool {
	// secret should be rotated before it expired.
	secretLifeTime := secret.ExpireTime.Sub(secret.CreatedTime)
	gracePeriod := time.Duration(sc.configOptions.SecretRotationGracePeriodRatio * float64(secretLifeTime))
	rotate := time.Now().After(secret.ExpireTime.Add(-gracePeriod))
	cacheLog.Debugf("Secret %s: lifetime: %v, graceperiod: %v, expiration: %v, should rotate: %v",
		secret.ResourceName, secretLifeTime, gracePeriod, secret.ExpireTime, rotate)
	return rotate
}

func (sc *SecretCache) isTokenExpired(secret *security.SecretItem) bool {
	// Skip check if the token should not be parsed in proxy.
	// Parsing token may not always be possible because token may not be a JWT.
	// If SkipParseToken is true, we should assume token is valid and leave token validation to CA.
	if sc.configOptions.SkipParseToken {
		return false
	}

	expired, err := util.IsJwtExpired(secret.Token, time.Now())
	if err != nil {
		cacheLog.Errorf("JWT expiration checking error: %v. Consider as expired.", err)
		return true
	}
	return expired
}

// sendRetriableRequest sends retriable requests for either CSR or ExchangeToken.
// Prior to sending the request, it also sleep random millisecond to avoid thundering herd problem.
func (sc *SecretCache) sendRetriableRequest(ctx context.Context, csrPEM []byte,
	providedExchangedToken string, connKey ConnKey, isCSR bool) ([]string, error) {

	if sc.configOptions.InitialBackoffInMilliSec > 0 {
		sc.randMutex.Lock()
		randomizedInitialBackOffInMS := sc.rand.Int63n(sc.configOptions.InitialBackoffInMilliSec)
		sc.randMutex.Unlock()
		cacheLog.Debugf("Wait for %d millisec for jitter", randomizedInitialBackOffInMS)
		// Add a jitter to initial CSR to avoid thundering herd problem.
		time.Sleep(time.Duration(randomizedInitialBackOffInMS) * time.Millisecond)
	}
	retryBackoffInMS := int64(firstRetryBackOffInMilliSec)

	// Assign a unique request ID for all the retries.
	reqID := uuid.New().String()

	logPrefix := cacheLogPrefixWithReqID(connKey.ResourceName, reqID)
	startTime := time.Now()
	var certChainPEM []string
	exchangedToken := providedExchangedToken
	var requestErrorString string
	var err error

	// Keep trying until no error or timeout.
	for {
		var httpRespCode int
		if isCSR {
			requestErrorString = fmt.Sprintf("%s CSR", logPrefix)
			// Check if we can use cert instead of the token to do CSR Sign request.
			if sc.useCertToRotate() {
				// if CSR request is without token, set the token to empty
				exchangedToken = ""
			}
			certChainPEM, err = sc.fetcher.CaClient.CSRSign(
				ctx, reqID, csrPEM, exchangedToken, int64(sc.configOptions.SecretTTL.Seconds()))
		} else {
			requestErrorString = fmt.Sprintf("%s TokExch", logPrefix)
			p := sc.configOptions.TokenExchangers[0]
			exchangedToken, _, httpRespCode, err = p.ExchangeToken(ctx, sc.configOptions.CredFetcher, sc.configOptions.TrustDomain, exchangedToken)
		}
		cacheLog.Debugf("%s", requestErrorString)

		if err == nil {
			break
		}

		// If non-retryable error, fail the request by returning err
		if !isRetryableErr(status.Code(err), httpRespCode, isCSR) {
			cacheLog.Errorf("%s hit non-retryable error (HTTP code: %d). Error: %v", requestErrorString, httpRespCode, err)
			return nil, err
		}

		// If reach envoy timeout, fail the request by returning err
		if startTime.Add(totalTimeout).Before(time.Now()) {
			cacheLog.Errorf("%s retrial timed out: %v", requestErrorString, err)
			return nil, err
		}
		time.Sleep(time.Duration(retryBackoffInMS) * time.Millisecond)
		cacheLog.Warnf("%s failed with error: %v, retry in %d millisec", requestErrorString, err, retryBackoffInMS)
		retryBackoffInMS *= 2 // Exponentially increase the retry backoff time.

		// Record retry metrics.
		if isCSR {
			numOutgoingRetries.With(RequestType.Value(CSR)).Increment()
		} else {
			numOutgoingRetries.With(RequestType.Value(TokenExchange)).Increment()
		}
	}

	if isCSR {
		return certChainPEM, nil
	}
	return []string{exchangedToken}, nil
}

// getExchangedToken gets the exchanged token for the CSR. The token is either the k8s jwt token of the
// workload or another token from a plug in provider.
func (sc *SecretCache) getExchangedToken(ctx context.Context, k8sJwtToken string, connKey ConnKey) (string, error) {
	logPrefix := cacheLogPrefix(connKey.ResourceName)
	cacheLog.Debugf("Start token exchange process for %s", logPrefix)
	if sc.configOptions.TokenExchangers == nil || len(sc.configOptions.TokenExchangers) == 0 {
		cacheLog.Debugf("Return k8s token for %s", logPrefix)
		return k8sJwtToken, nil
	}
	if len(sc.configOptions.TokenExchangers) > 1 {
		cacheLog.Errorf("Found more than one plugin for %s", logPrefix)
		return "", fmt.Errorf("found more than one plugin")
	}
	exchangedTokens, err := sc.sendRetriableRequest(ctx, nil, k8sJwtToken,
		ConnKey{ConnectionID: "", ResourceName: ""}, false)
	if err != nil || len(exchangedTokens) == 0 {
		cacheLog.Errorf("Failed to exchange token for %s: %v", logPrefix, err)
		return "", err
	}
	cacheLog.Debugf("Token exchange succeeded for %s", logPrefix)
	return exchangedTokens[0], nil
}

// useCertToRotate checks if we can use cert instead of token to do CSR.
func (sc *SecretCache) useCertToRotate() bool {
	// Check if CA requires a token in CSR
	if sc.configOptions.UseTokenForCSR {
		return false
	}
	if sc.configOptions.ProvCert == "" {
		return false
	}
	// Check if cert exists.
	_, err := tls.LoadX509KeyPair(sc.configOptions.ProvCert+"/cert-chain.pem", sc.configOptions.ProvCert+"/key.pem")
	if err != nil {
		cacheLog.Errorf("cannot load key pair from %s: %s", sc.configOptions.ProvCert, err)
		return false
	}
	return true
}
