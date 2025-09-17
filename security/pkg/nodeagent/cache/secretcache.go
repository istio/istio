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
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"istio.io/istio/pkg/backoff"
	"istio.io/istio/pkg/file"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/queue"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/security/pkg/monitoring"
	nodeagentutil "istio.io/istio/security/pkg/nodeagent/util"
	pkiutil "istio.io/istio/security/pkg/pki/util"
)

var (
	cacheLog = istiolog.RegisterScope("cache", "cache debugging")
	// The total timeout for any credential retrieval process, default value of 10s is used.
	totalTimeout = time.Second * 10
)

// isPathInSymlink checks if any part of the file path is a symlink
func (sc *SecretManagerClient) isPathInSymlink(filePath string) bool {
	// First check if the file itself is a symlink
	fileInfo, err := os.Lstat(filePath)
	if err == nil && fileInfo.Mode()&os.ModeSymlink != 0 {
		return true
	}

	// Then check parent directories, but only go up a few levels to avoid system symlinks
	dir := filepath.Dir(filePath)
	depth := 0
	maxDepth := 5 // Only check a few parent directories

	for dir != "." && dir != "/" && dir != filepath.Dir(dir) && depth < maxDepth {
		fileInfo, err := os.Lstat(dir)
		if err != nil {
			break
		}
		if fileInfo.Mode()&os.ModeSymlink != 0 {
			return true
		}
		dir = filepath.Dir(dir)
		depth++
	}
	return false
}

// findSymlinkInPath finds the symlink component in a file path
func (sc *SecretManagerClient) findSymlinkInPath(filePath string) string {
	// First check if the file itself is a symlink
	fileInfo, err := os.Lstat(filePath)
	if err == nil && fileInfo.Mode()&os.ModeSymlink != 0 {
		return filePath
	}

	// Then check parent directories, but only go up a few levels to avoid system symlinks
	dir := filepath.Dir(filePath)
	depth := 0
	maxDepth := 5 // Only check a few parent directories

	for dir != "." && dir != "/" && dir != filepath.Dir(dir) && depth < maxDepth {
		fileInfo, err := os.Lstat(dir)
		if err != nil {
			break
		}
		if fileInfo.Mode()&os.ModeSymlink != 0 {
			return dir
		}
		dir = filepath.Dir(dir)
		depth++
	}
	return ""
}

// resolveSymlink resolves a symlink to its target path and determines if it's a directory
func (sc *SecretManagerClient) resolveSymlink(symlinkPath string) (string, bool, error) {
	targetPath, err := os.Readlink(symlinkPath)
	if err != nil {
		return "", false, err
	}

	// If target is relative, make it absolute relative to the symlink's directory
	if !filepath.IsAbs(targetPath) {
		symlinkDir := filepath.Dir(symlinkPath)
		targetPath = filepath.Join(symlinkDir, targetPath)
	}

	// Check if target is a directory
	fileInfo, err := os.Stat(targetPath)
	if err != nil {
		return targetPath, false, err
	}

	return targetPath, fileInfo.IsDir(), nil
}

const (
	// firstRetryBackOffDuration is the initial backoff time interval when hitting
	// non-retryable error in CSR request or while there is an error in reading file mounts.
	firstRetryBackOffDuration = 50 * time.Millisecond
)

// SecretManagerClient a SecretManager that signs CSRs using a provided security.Client. The primary
// usage is to fetch the two specially named resources: `default`, which refers to the workload's
// spiffe certificate, and ROOTCA, which contains just the root certificate for the workload
// certificates. These are separated only due to the fact that Envoy has them separated.
// Additionally, arbitrary certificates may be fetched from local files to support DestinationRule
// and Gateway. Note that certificates stored externally will be sent from Istiod directly; the
// in-agent SecretManagerClient has low privileges and cannot read Kubernetes Secrets or other
// storage backends. Istiod is in charge of determining whether the agent (ie SecretManagerClient) or
// Istiod will serve an SDS response, by selecting the appropriate cluster in the SDS configuration
// it serves.
//
// SecretManagerClient supports two modes of retrieving certificate (potentially at the same time):
//   - File based certificates. If certs are mounted under well-known path /etc/certs/{key,cert,root-cert.pem},
//     requests for `default` and `ROOTCA` will automatically read from these files. Additionally,
//     certificates from Gateway/DestinationRule can also be served. This is done by parsing resource
//     names in accordance with security.SdsCertificateConfig (file-cert: and file-root:).
//   - On demand CSRs. This is used only for the `default` certificate. When this resource is
//     requested, a CSR will be sent to the configured caClient.
//
// Callers are expected to only call GenerateSecret when a new certificate is required. Generally,
// this should be done a single time at startup, then repeatedly when the certificate is near
// expiration. To help users handle certificate expiration, any certificates created by the caClient
// will be monitored; when they are near expiration the secretHandler function is triggered,
// prompting the client to call GenerateSecret again, if they still care about the certificate. For
// files, this callback is instead triggered on any change to the file (triggering on expiration
// would not be helpful, as all we can do is re-read the same file).
type SecretManagerClient struct {
	caClient security.Client

	// configOptions includes all configurable params for the cache.
	configOptions *security.Options

	// callback function to invoke when detecting secret change.
	secretHandler func(resourceName string)

	// Cache of workload certificate and root certificate. File based certs are never cached, as
	// lookup is cheap.
	cache secretCache

	// generateMutex ensures we do not send concurrent requests to generate a certificate
	generateMutex sync.Mutex

	// The paths for an existing certificate chain, key and root cert files. Istio agent will
	// use them as the source of secrets if they exist.
	existingCertificateFile security.SdsCertificateConfig

	// certWatcher watches the certificates for changes and triggers a notification to proxy.
	certWatcher *fsnotify.Watcher
	// certs being watched with file watcher.
	fileCerts map[FileCert]struct{}
	certMutex sync.RWMutex

	// outputMutex protects writes of certificates to disk
	outputMutex sync.Mutex

	// Dynamically configured Trust Bundle Mutex
	configTrustBundleMutex sync.RWMutex
	// Dynamically configured Trust Bundle
	configTrustBundle []byte

	// queue maintains all certificate rotation events that need to be triggered when they are about to expire
	queue queue.Delayed
	stop  chan struct{}

	caRootPath string
}

type secretCache struct {
	mu       sync.RWMutex
	workload *security.SecretItem
	certRoot []byte
}

// GetRoot returns cached root cert and cert expiration time. This method is thread safe.
func (s *secretCache) GetRoot() (rootCert []byte) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.certRoot
}

// SetRoot sets root cert into cache. This method is thread safe.
func (s *secretCache) SetRoot(rootCert []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.certRoot = rootCert
}

func (s *secretCache) GetWorkload() *security.SecretItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.workload == nil {
		return nil
	}
	return s.workload
}

func (s *secretCache) SetWorkload(value *security.SecretItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workload = value
}

var _ security.SecretManager = &SecretManagerClient{}

// FileCert stores a reference to a certificate on disk
type FileCert struct {
	ResourceName string
	Filename     string
	// For symlinks, this stores the resolved target path
	TargetPath string
}

// NewSecretManagerClient creates a new SecretManagerClient.
func NewSecretManagerClient(caClient security.Client, options *security.Options) (*SecretManagerClient, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	ret := &SecretManagerClient{
		queue:         queue.NewDelayed(queue.DelayQueueBuffer(0)),
		caClient:      caClient,
		configOptions: options,
		existingCertificateFile: security.SdsCertificateConfig{
			CertificatePath:   options.CertChainFilePath,
			PrivateKeyPath:    options.KeyFilePath,
			CaCertificatePath: options.RootCertFilePath,
		},
		certWatcher: watcher,
		fileCerts:   make(map[FileCert]struct{}),
		stop:        make(chan struct{}),
		caRootPath:  options.CARootPath,
	}

	go ret.queue.Run(ret.stop)
	go ret.handleFileWatch()
	return ret, nil
}

func (sc *SecretManagerClient) Close() {
	_ = sc.certWatcher.Close()
	if sc.caClient != nil {
		sc.caClient.Close()
	}
	close(sc.stop)
}

func (sc *SecretManagerClient) RegisterSecretHandler(h func(resourceName string)) {
	sc.certMutex.Lock()
	defer sc.certMutex.Unlock()
	sc.secretHandler = h
}

func (sc *SecretManagerClient) OnSecretUpdate(resourceName string) {
	cacheLog.Infof("OnSecretUpdate called for resource: %s", resourceName)
	sc.certMutex.RLock()
	defer sc.certMutex.RUnlock()
	if sc.secretHandler != nil {
		cacheLog.Infof("calling secretHandler for resource: %s", resourceName)
		sc.secretHandler(resourceName)
	}
}

// getCachedSecret: retrieve cached Secret Item (workload-certificate/workload-root) from secretManager client
func (sc *SecretManagerClient) getCachedSecret(resourceName string) (secret *security.SecretItem) {
	var rootCertBundle []byte
	var ns *security.SecretItem

	if c := sc.cache.GetWorkload(); c != nil {
		if resourceName == security.RootCertReqResourceName {
			rootCertBundle = sc.mergeTrustAnchorBytes(c.RootCert)
			ns = &security.SecretItem{
				ResourceName: resourceName,
				RootCert:     rootCertBundle,
			}
			cacheLog.WithLabels("ttl", time.Until(c.ExpireTime)).Info("returned workload trust anchor from cache")

		} else {
			ns = &security.SecretItem{
				ResourceName:     resourceName,
				CertificateChain: c.CertificateChain,
				PrivateKey:       c.PrivateKey,
				ExpireTime:       c.ExpireTime,
				CreatedTime:      c.CreatedTime,
			}
			cacheLog.WithLabels("ttl", time.Until(c.ExpireTime)).Info("returned workload certificate from cache")
		}

		return ns
	}
	return nil
}

// GenerateSecret passes the cached secret to SDS.StreamSecrets and SDS.FetchSecret.
func (sc *SecretManagerClient) GenerateSecret(resourceName string) (secret *security.SecretItem, err error) {
	cacheLog.Infof("GenerateSecret called for resource: %s", resourceName)
	// Setup the call to store generated secret to disk
	defer func() {
		if secret == nil || err != nil {
			return
		}
		// We need to hold a mutex here, otherwise if two threads are writing the same certificate,
		// we may permanently end up with a mismatch key/cert pair. We still make end up temporarily
		// with mismatched key/cert pair since we cannot atomically write multiple files. It may be
		// possible by keeping the output in a directory with clever use of symlinks in the future,
		// if needed.
		sc.outputMutex.Lock()
		defer sc.outputMutex.Unlock()
		if resourceName == security.RootCertReqResourceName || resourceName == security.WorkloadKeyCertResourceName {
			if err := nodeagentutil.OutputKeyCertToDir(sc.configOptions.OutputKeyCertToDir, secret.PrivateKey,
				secret.CertificateChain, secret.RootCert); err != nil {
				cacheLog.Errorf("error when output the resource: %v", err)
			} else if sc.configOptions.OutputKeyCertToDir != "" {
				resourceLog(resourceName).Debugf("output the resource to %v", sc.configOptions.OutputKeyCertToDir)
			}
		}
	}()

	t0 := time.Now()
	sc.generateMutex.Lock()
	defer sc.generateMutex.Unlock()

	// First try to generate secret from file.
	if sdsFromFile, ns, err := sc.generateFileSecret(resourceName); sdsFromFile {
		if err != nil {
			return nil, err
		}
		return ns, nil
	}

	ns := sc.getCachedSecret(resourceName)
	if ns != nil {
		return ns, nil
	}

	// Now that we got the lock, look at cache again before sending request to avoid overwhelming CA
	ns = sc.getCachedSecret(resourceName)
	if ns != nil {
		return ns, nil
	}

	if ts := time.Since(t0); ts > time.Second {
		cacheLog.Warnf("slow generate secret lock: %v", ts)
	}

	// send request to CA to get new workload certificate
	ns, err = sc.generateNewSecret(resourceName)
	if err != nil {
		return nil, fmt.Errorf("failed to generate workload certificate: %v", err)
	}

	// Store the new secret in the secretCache and trigger the periodic rotation for workload certificate
	sc.registerSecret(*ns)

	if resourceName == security.RootCertReqResourceName {
		ns.RootCert = sc.mergeTrustAnchorBytes(ns.RootCert)
	} else {
		// If periodic cert refresh resulted in discovery of a new root, trigger a ROOTCA request to refresh trust anchor
		oldRoot := sc.cache.GetRoot()
		if !bytes.Equal(oldRoot, ns.RootCert) {
			cacheLog.Info("Root cert has changed, start rotating root cert")
			// We store the oldRoot only for comparison and not for serving
			sc.cache.SetRoot(ns.RootCert)
			sc.OnSecretUpdate(security.RootCertReqResourceName)
		}
	}

	return ns, nil
}

// addSymlinkWatcher adds watchers for both the symlink and its target
func (sc *SecretManagerClient) addSymlinkWatcher(filePath string, resourceName string) error {
	sc.certMutex.Lock()
	defer sc.certMutex.Unlock()

	absFilePath, err := filepath.Abs(filePath)
	if err != nil {
		cacheLog.Errorf("%v: error finding absolute path of file %s, retrying watches: %v", resourceName, filePath, err)
		return err
	}

	// Find the symlink in the path
	symlinkPath := sc.findSymlinkInPath(absFilePath)
	if symlinkPath == "" {
		cacheLog.Errorf("%v: no symlink found in path %s", resourceName, absFilePath)
		return fmt.Errorf("no symlink found in path")
	}

	// Resolve the symlink
	targetPath, _, err := sc.resolveSymlink(symlinkPath)
	if err != nil {
		cacheLog.Errorf("%v: error resolving symlink %s: %v", resourceName, symlinkPath, err)
		return err
	}

	key := FileCert{
		ResourceName: resourceName,
		Filename:     absFilePath,
		TargetPath:   targetPath,
	}

	// Check if we're already watching this symlink
	if _, alreadyWatching := sc.fileCerts[key]; alreadyWatching {
		cacheLog.Debugf("already watching symlink for %s", absFilePath)
		return nil
	}

	// Store symlink information
	sc.fileCerts[key] = struct{}{}

	// Add watcher for the symlink itself
	cacheLog.Infof("adding watcher for symlink certificate %s -> %s", absFilePath, targetPath)
	if err := sc.certWatcher.Add(symlinkPath); err != nil {
		cacheLog.Errorf("%v: error adding watcher for symlink %v, retrying watches: %v", resourceName, symlinkPath, err)
		delete(sc.fileCerts, key)
		numFileWatcherFailures.Increment()
		return err
	}

	// Add watcher for the parent directory to detect when the symlink directory is removed/recreated
	parentDir := filepath.Dir(symlinkPath)
	if err := sc.certWatcher.Add(parentDir); err != nil {
		cacheLog.Errorf("%v: error adding watcher for symlink parent directory %v, retrying watches: %v", resourceName, parentDir, err)
		// Don't fail completely, we can still watch the symlink
	}

	// Also watch the grandparent directory to detect when the symlink directory itself is removed
	grandParentDir := filepath.Dir(parentDir)
	if grandParentDir != parentDir && grandParentDir != "." {
		if err := sc.certWatcher.Add(grandParentDir); err != nil {
			cacheLog.Errorf("%v: error adding watcher for symlink grandparent directory %v, retrying watches: %v", resourceName, grandParentDir, err)
			// Don't fail completely, we can still watch the symlink
		}
	}

	// Add watcher for the target
	if err := sc.certWatcher.Add(targetPath); err != nil {
		cacheLog.Errorf("%v: error adding watcher for symlink target %v, retrying watches: %v", resourceName, targetPath, err)
		// Don't fail completely, we can still watch the symlink
	}

	return nil
}

func (sc *SecretManagerClient) addFileWatcher(file string, resourceName string) {
	// Check if the file or any part of its path is a symlink
	if sc.isPathInSymlink(file) {
		if err := sc.addSymlinkWatcher(file, resourceName); err == nil {
			return
		}
		// Retry symlink watcher
		go func() {
			b := backoff.NewExponentialBackOff(backoff.DefaultOption())
			_ = b.RetryWithContext(context.TODO(), func() error {
				err := sc.addSymlinkWatcher(file, resourceName)
				return err
			})
		}()
		return
	}

	// Try adding file watcher and if it fails start a retry loop.
	if err := sc.tryAddFileWatcher(file, resourceName); err == nil {
		return
	}
	// RetryWithContext file watcher as some times it might fail to add and we will miss change
	// notifications on those files. For now, retry for ever till the watcher is added.
	// TODO(ramaraochavali): Think about tying these failures to liveness probe with a
	// reasonable threshold (when the problem is not transient) and restart the pod.
	go func() {
		b := backoff.NewExponentialBackOff(backoff.DefaultOption())
		_ = b.RetryWithContext(context.TODO(), func() error {
			err := sc.tryAddFileWatcher(file, resourceName)
			return err
		})
	}()
}

func (sc *SecretManagerClient) tryAddFileWatcher(file string, resourceName string) error {
	// Check if this file is being already watched, if so ignore it. This check is needed here to
	// avoid processing duplicate events for the same file.
	sc.certMutex.Lock()
	defer sc.certMutex.Unlock()
	file, err := filepath.Abs(file)
	if err != nil {
		cacheLog.Errorf("%v: error finding absolute path of %s, retrying watches: %v", resourceName, file, err)
		return err
	}
	key := FileCert{
		ResourceName: resourceName,
		Filename:     file,
		TargetPath:   "", // Empty for regular files
	}
	if _, alreadyWatching := sc.fileCerts[key]; alreadyWatching {
		cacheLog.Debugf("already watching file for %s", file)
		// Already watching, no need to do anything
		return nil
	}
	sc.fileCerts[key] = struct{}{}
	// File is not being watched, start watching now and trigger key push.
	cacheLog.Infof("adding watcher for file certificate %s", file)
	if err := sc.certWatcher.Add(file); err != nil {
		cacheLog.Errorf("%v: error adding watcher for file %v, retrying watches: %v", resourceName, file, err)
		numFileWatcherFailures.Increment()
		return err
	}
	return nil
}

// If there is existing root certificates under a well known path, return true.
// Otherwise, return false.
func (sc *SecretManagerClient) rootCertificateExist(filePath string) bool {
	b, err := os.ReadFile(filePath)
	if err != nil || len(b) == 0 {
		return false
	}
	return true
}

// If there is an existing private key and certificate under a well known path, return true.
// Otherwise, return false.
func (sc *SecretManagerClient) keyCertificateExist(certPath, keyPath string) bool {
	b, err := os.ReadFile(certPath)
	if err != nil || len(b) == 0 {
		return false
	}
	b, err = os.ReadFile(keyPath)
	if err != nil || len(b) == 0 {
		return false
	}

	return true
}

// Generate a root certificates item from the passed in rootCertPath,
// but ignore certificates that are not valid.
// This might happen if a root certificate has a negativeSerialNumber.
// Although rfc5280 does not allow negative serial numbers, but does require graceful handling
// (https://datatracker.ietf.org/doc/html/rfc5280#section-4.1.2.2)
// If there is an invalid cert, we ignore it and only error if there are no valid certs.
func (sc *SecretManagerClient) generateRootCertFromExistingFile(rootCertPath, resourceName string, workload bool) (*security.SecretItem, error) {
	var validRootCertBytes []byte
	o := backoff.DefaultOption()
	o.InitialInterval = sc.configOptions.FileDebounceDuration
	b := backoff.NewExponentialBackOff(o)
	parseCerts := func() error {
		rootCert, err := os.ReadFile(rootCertPath)
		if err != nil {
			return err
		}

		var errs []error
		validRootCertBytes, errs = pkiutil.ParseRootCerts(rootCert)
		if len(validRootCertBytes) == 0 {
			return fmt.Errorf("failed to parse root certs from file %s: %v", rootCertPath, errs)
		}
		if len(errs) > 0 {
			cacheLog.Errorf("failed to parse some certs from file %s: %v", rootCertPath, errs)
		}

		cacheLog.Infof("loaded root certificate from file %s", rootCertPath)

		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), totalTimeout)
	defer cancel()
	if err := b.RetryWithContext(ctx, parseCerts); err != nil {
		return nil, err
	}

	// Set the rootCert only if it is workload root cert.
	if workload {
		sc.cache.SetRoot(validRootCertBytes)
	}
	return &security.SecretItem{
		ResourceName: resourceName,
		RootCert:     validRootCertBytes,
	}, nil
}

// Generate a key and certificate item from the existing key certificate files from the passed in file paths.
func (sc *SecretManagerClient) generateKeyCertFromExistingFiles(certChainPath, keyPath, resourceName string) (*security.SecretItem, error) {
	// There is a remote possibility that key is written and cert is not written yet.
	// To handle that case, check if cert and key are valid if they are valid then only send to proxy.
	o := backoff.DefaultOption()
	o.InitialInterval = sc.configOptions.FileDebounceDuration
	b := backoff.NewExponentialBackOff(o)
	secretValid := func() error {
		_, err := tls.LoadX509KeyPair(certChainPath, keyPath)
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), totalTimeout)
	defer cancel()
	if err := b.RetryWithContext(ctx, secretValid); err != nil {
		return nil, err
	}
	return sc.keyCertSecretItem(certChainPath, keyPath, resourceName)
}

func (sc *SecretManagerClient) keyCertSecretItem(cert, key, resource string) (*security.SecretItem, error) {
	certChain, err := sc.readFileWithTimeout(cert)
	if err != nil {
		return nil, err
	}
	keyPEM, err := sc.readFileWithTimeout(key)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	var certExpireTime time.Time
	if certExpireTime, err = nodeagentutil.ParseCertAndGetExpiryTimestamp(certChain); err != nil {
		cacheLog.Errorf("failed to extract expiration time in the certificate loaded from file: %v", err)
		return nil, fmt.Errorf("failed to extract expiration time in the certificate loaded from file: %v", err)
	}

	cacheLog.Infof("loaded certificate chain from file %s", cert)

	return &security.SecretItem{
		CertificateChain: certChain,
		PrivateKey:       keyPEM,
		ResourceName:     resource,
		CreatedTime:      now,
		ExpireTime:       certExpireTime,
	}, nil
}

// readFileWithTimeout reads the given file with timeout. It returns error
// if it is not able to read file after timeout.
func (sc *SecretManagerClient) readFileWithTimeout(path string) ([]byte, error) {
	retryBackoff := firstRetryBackOffDuration
	timeout := time.After(totalTimeout)
	for {
		cert, err := os.ReadFile(path)
		if err == nil {
			return cert, nil
		}
		select {
		case <-time.After(retryBackoff):
			retryBackoff *= 2
		case <-timeout:
			return nil, err
		case <-sc.stop:
			return nil, err
		}
	}
}

func (sc *SecretManagerClient) generateFileSecret(resourceName string) (bool, *security.SecretItem, error) {
	logPrefix := cacheLogPrefix(resourceName)

	cf := sc.existingCertificateFile
	// outputToCertificatePath handles a special case where we have configured to output certificates
	// to the special /etc/certs directory. In this case, we need to ensure we do *not* read from
	// these files, otherwise we would never rotate.
	outputToCertificatePath, ferr := file.DirEquals(filepath.Dir(cf.CertificatePath), sc.configOptions.OutputKeyCertToDir)
	if ferr != nil {
		return false, nil, ferr
	}
	// When there are existing root certificates, or private key and certificate under
	// a well known path, they are used in the SDS response.
	sdsFromFile := false
	var err error
	var sitem *security.SecretItem

	switch {
	// Default root certificate.
	case resourceName == security.RootCertReqResourceName && sc.rootCertificateExist(cf.CaCertificatePath) && !outputToCertificatePath:
		sdsFromFile = true
		if sitem, err = sc.generateRootCertFromExistingFile(cf.CaCertificatePath, resourceName, true); err == nil {
			// If retrieving workload trustBundle, then merge other configured trustAnchors in ProxyConfig
			sitem.RootCert = sc.mergeTrustAnchorBytes(sitem.RootCert)
			sc.addFileWatcher(cf.CaCertificatePath, resourceName)
		}
	// Default workload certificate.
	case resourceName == security.WorkloadKeyCertResourceName && sc.keyCertificateExist(cf.CertificatePath, cf.PrivateKeyPath) && !outputToCertificatePath:
		sdsFromFile = true
		if sitem, err = sc.generateKeyCertFromExistingFiles(cf.CertificatePath, cf.PrivateKeyPath, resourceName); err == nil {
			// Adding cert is sufficient here as key can't change without changing the cert.
			sc.addFileWatcher(cf.CertificatePath, resourceName)
		}
	case resourceName == security.FileRootSystemCACert:
		sdsFromFile = true
		if sc.caRootPath != "" {
			if sitem, err = sc.generateRootCertFromExistingFile(sc.caRootPath, resourceName, false); err == nil {
				sc.addFileWatcher(sc.caRootPath, resourceName)
			}
		} else {
			sdsFromFile = false
		}
	default:
		// Check if the resource name refers to a file mounted certificate.
		// Currently used in destination rules and server certs (via metadata).
		// Based on the resource name, we need to read the secret from a file encoded in the resource name.
		cfg, ok := security.SdsCertificateConfigFromResourceName(resourceName)
		sdsFromFile = ok
		switch {
		case ok && cfg.IsRootCertificate():
			if sitem, err = sc.generateRootCertFromExistingFile(cfg.CaCertificatePath, resourceName, false); err == nil {
				sc.addFileWatcher(cfg.CaCertificatePath, resourceName)
			}
		case ok && cfg.IsKeyCertificate():
			if sitem, err = sc.generateKeyCertFromExistingFiles(cfg.CertificatePath, cfg.PrivateKeyPath, resourceName); err == nil {
				// Adding cert is sufficient here as key can't change without changing the cert.
				sc.addFileWatcher(cfg.CertificatePath, resourceName)
			}
		}
	}

	if sdsFromFile {
		if err != nil {
			cacheLog.Errorf("%s failed to generate secret for proxy from file: %v",
				logPrefix, err)
			numFileSecretFailures.Increment()
			return sdsFromFile, nil, err
		}
		// We do not register the secret. Unlike on-demand CSRs, there is nothing we can do if a file
		// cert expires; there is no point sending an update when its near expiry. Instead, a
		// separate file watcher will ensure if the file changes we trigger an update.
		return sdsFromFile, sitem, nil
	}
	return sdsFromFile, nil, nil
}

func (sc *SecretManagerClient) generateNewSecret(resourceName string) (*security.SecretItem, error) {
	trustBundlePEM := []string{}
	var rootCertPEM []byte

	if sc.caClient == nil {
		return nil, fmt.Errorf("attempted to fetch secret, but ca client is nil")
	}
	t0 := time.Now()
	logPrefix := cacheLogPrefix(resourceName)

	csrHostName := &spiffe.Identity{
		TrustDomain:    sc.configOptions.TrustDomain,
		Namespace:      sc.configOptions.WorkloadNamespace,
		ServiceAccount: sc.configOptions.ServiceAccount,
	}

	cacheLog.Debugf("%s constructed host name for CSR: %s", logPrefix, csrHostName.String())
	options := pkiutil.CertOptions{
		Host:       csrHostName.String(),
		RSAKeySize: sc.configOptions.WorkloadRSAKeySize,
		PKCS8Key:   sc.configOptions.Pkcs8Keys,
		ECSigAlg:   pkiutil.SupportedECSignatureAlgorithms(sc.configOptions.ECCSigAlg),
		ECCCurve:   pkiutil.SupportedEllipticCurves(sc.configOptions.ECCCurve),
	}

	// Generate the cert/key, send CSR to CA.
	csrPEM, keyPEM, err := pkiutil.GenCSR(options)
	if err != nil {
		cacheLog.Errorf("%s failed to generate key and certificate for CSR: %v", logPrefix, err)
		return nil, err
	}

	numOutgoingRequests.With(RequestType.Value(monitoring.CSR)).Increment()
	timeBeforeCSR := time.Now()
	certChainPEM, err := sc.caClient.CSRSign(csrPEM, int64(sc.configOptions.SecretTTL.Seconds()))
	if err == nil {
		trustBundlePEM, err = sc.caClient.GetRootCertBundle()
	}
	csrLatency := float64(time.Since(timeBeforeCSR).Nanoseconds()) / float64(time.Millisecond)
	outgoingLatency.With(RequestType.Value(monitoring.CSR)).Record(csrLatency)
	if err != nil {
		numFailedOutgoingRequests.With(RequestType.Value(monitoring.CSR)).Increment()
		cacheLog.Errorf("%s failed to sign: %v", logPrefix, err)
		return nil, err
	}

	certChain := concatCerts(certChainPEM)

	var expireTime time.Time
	// Cert expire time by default is createTime + sc.configOptions.SecretTTL.
	// Istiod respects SecretTTL that passed to it and use it decide TTL of cert it issued.
	// Some customer CA may override TTL param that's passed to it.
	if expireTime, err = nodeagentutil.ParseCertAndGetExpiryTimestamp(certChain); err != nil {
		cacheLog.Errorf("%s failed to extract expire time from server certificate in CSR response %+v: %v",
			logPrefix, certChainPEM, err)
		return nil, fmt.Errorf("failed to extract expire time from server certificate in CSR response: %v", err)
	}

	cacheLog.WithLabels("resourceName", resourceName,
		"latency", time.Since(t0),
		"ttl", time.Until(expireTime)).
		Info("generated new workload certificate")

	if len(trustBundlePEM) > 0 {
		rootCertPEM = concatCerts(trustBundlePEM)
	} else {
		// If CA Client has no explicit mechanism to retrieve CA root, infer it from the root of the certChain
		rootCertPEM = []byte(certChainPEM[len(certChainPEM)-1])
	}

	return &security.SecretItem{
		CertificateChain: certChain,
		PrivateKey:       keyPEM,
		ResourceName:     resourceName,
		CreatedTime:      time.Now(),
		ExpireTime:       expireTime,
		RootCert:         rootCertPEM,
	}, nil
}

var rotateTime = func(secret security.SecretItem, graceRatio float64, graceRatioJitter float64) time.Duration {
	// stagger rotation times to prevent large fleets of clients from renewing at the same moment.
	jitter := (rand.Float64() * graceRatioJitter) * float64(rand.IntN(2)*2-1) // #nosec G404 -- crypto/rand not worth the cost
	jitterGraceRatio := graceRatio + jitter
	if jitterGraceRatio > 1 {
		jitterGraceRatio = 1
	}
	if jitterGraceRatio < 0 {
		jitterGraceRatio = 0
	}
	secretLifeTime := secret.ExpireTime.Sub(secret.CreatedTime)
	gracePeriod := time.Duration((jitterGraceRatio) * float64(secretLifeTime))
	delay := time.Until(secret.ExpireTime.Add(-gracePeriod))
	if delay < 0 {
		delay = 0
	}
	return delay
}

func (sc *SecretManagerClient) registerSecret(item security.SecretItem) {
	delay := rotateTime(item, sc.configOptions.SecretRotationGracePeriodRatio, sc.configOptions.SecretRotationGracePeriodRatioJitter)
	item.ResourceName = security.WorkloadKeyCertResourceName
	// In case there are two calls to GenerateSecret at once, we don't want both to be concurrently registered
	if sc.cache.GetWorkload() != nil {
		resourceLog(item.ResourceName).Infof("skip scheduling certificate rotation, already scheduled")
		return
	}
	sc.cache.SetWorkload(&item)
	resourceLog(item.ResourceName).Debugf("scheduled certificate for rotation in %v", delay)
	certExpirySeconds.ValueFrom(func() float64 { return time.Until(item.ExpireTime).Seconds() }, ResourceName.Value(item.ResourceName))
	sc.queue.PushDelayed(func() error {
		// In case `UpdateConfigTrustBundle` called, it will resign workload cert.
		// Check if this is a stale scheduled rotating task.
		if cached := sc.cache.GetWorkload(); cached != nil {
			if cached.CreatedTime == item.CreatedTime {
				resourceLog(item.ResourceName).Debugf("rotating certificate")
				// Clear the cache so the next call generates a fresh certificate
				sc.cache.SetWorkload(nil)
				sc.OnSecretUpdate(item.ResourceName)
			}
		}
		return nil
	}, delay)
}

func (sc *SecretManagerClient) handleFileWatch() {
	for {
		select {
		case event, ok := <-sc.certWatcher.Events:
			// Channel is closed.
			if !ok {
				return
			}
			// We only care about updates that change the file content
			if !(isWrite(event) || isRemove(event) || isCreate(event)) {
				continue
			}
			sc.certMutex.RLock()
			resources := make(map[FileCert]struct{})
			for k, v := range sc.fileCerts {
				resources[k] = v
			}
			sc.certMutex.RUnlock()
			cacheLog.Infof("event for file certificate %s : %s, pushing to proxy", event.Name, event.Op.String())

			// Handle symlink events first and track which resources need updates
			updatedResources := sc.handleSymlinkEvent(event, resources)

			// If it is remove event - cleanup from file certs so that if it is added again, we can watch.
			// The cleanup should happen first before triggering callbacks, as the callbacks are async and
			// we may get generate call before cleanup is done and we will end up not watching the file.
			if isRemove(event) {
				sc.certMutex.Lock()
				for fc := range sc.fileCerts {
					if fc.Filename == event.Name {
						cacheLog.Debugf("removing file %s from file certs", event.Name)
						delete(sc.fileCerts, fc)
						break
					}
				}
				sc.certMutex.Unlock()
			}

			// Trigger callbacks for all resources that need updates
			for resourceName := range updatedResources {
				cacheLog.Infof("triggering certificate update for resource %s due to file change: %s (event: %s)", resourceName, event.Name, event.Op.String())
				sc.OnSecretUpdate(resourceName)
			}
		case err, ok := <-sc.certWatcher.Errors:
			// Channel is closed.
			if !ok {
				return
			}
			numFileWatcherFailures.Increment()
			cacheLog.Errorf("certificate watch error: %v", err)
		}
	}
}

// handleSymlinkEvent handles file system events for symlinks and returns resources that need updates
func (sc *SecretManagerClient) handleSymlinkEvent(event fsnotify.Event, resources map[FileCert]struct{}) map[string]struct{} {
	updatedResources := make(map[string]struct{})

	// Check if this event is for a symlink we're watching
	for fc := range resources {
		// Only process symlinks (those with TargetPath set)
		if fc.TargetPath == "" {
			continue
		}

		// Check if the event is for the symlink itself, its target, or its parent/grandparent directory
		parentDir := filepath.Dir(fc.Filename)
		grandParentDir := filepath.Dir(parentDir)
		if fc.Filename == event.Name || fc.TargetPath == event.Name || parentDir == event.Name || grandParentDir == event.Name {
			cacheLog.Infof("symlink event for %s (symlink: %s, target: %s, parent: %s, grandparent: %s): %s",
				event.Name, fc.Filename, fc.TargetPath, parentDir, grandParentDir, event.Op.String())

			// If the symlink itself, its parent directory, or grandparent directory changed (removed/recreated), we need to re-resolve it
			if (fc.Filename == event.Name || parentDir == event.Name || grandParentDir == event.Name) && (isRemove(event) || isCreate(event)) {
				cacheLog.Infof("handling symlink change for resource %s", fc.ResourceName)
				sc.handleSymlinkChange(fc)
			}

			// Only trigger updates for symlink file changes or directory changes, not target file changes
			// This prevents duplicate updates when both symlink and target files change
			if fc.Filename == event.Name || parentDir == event.Name || grandParentDir == event.Name {
				updatedResources[fc.ResourceName] = struct{}{}
			}
		}
	}

	// Also check for regular files
	for fc := range resources {
		// Only process regular files (those without TargetPath set)
		if fc.TargetPath != "" {
			continue
		}

		// For regular files, trigger on file changes
		if fc.Filename == event.Name {
			updatedResources[fc.ResourceName] = struct{}{}
		}
	}

	return updatedResources
}

// handleSymlinkChange handles when a symlink is removed and recreated
func (sc *SecretManagerClient) handleSymlinkChange(fc FileCert) {
	sc.certMutex.Lock()
	defer sc.certMutex.Unlock()

	// Find the symlink directory path (not the file path)
	symlinkDir := filepath.Dir(fc.Filename)

	// Try to re-resolve the symlink directory
	newTargetPath, _, err := sc.resolveSymlink(symlinkDir)
	if err != nil {
		cacheLog.Errorf("failed to re-resolve symlink directory %s: %v", symlinkDir, err)
		// Don't return early, we still want to update the entry with the old target
		newTargetPath = fc.TargetPath
	}

	// Update the symlink info in the fileCerts map
	// First remove the old entry
	delete(sc.fileCerts, fc)

	// Create new entry with updated target
	newFc := FileCert{
		ResourceName: fc.ResourceName,
		Filename:     fc.Filename,
		TargetPath:   newTargetPath,
	}
	sc.fileCerts[newFc] = struct{}{}

	// If the target changed, add watcher for the new target
	if newTargetPath != fc.TargetPath {
		cacheLog.Infof("symlink target changed from %s to %s", fc.TargetPath, newTargetPath)
		if err := sc.certWatcher.Add(newTargetPath); err != nil {
			cacheLog.Errorf("error adding watcher for new symlink target %s: %v", newTargetPath, err)
		}
	}
}

func isWrite(event fsnotify.Event) bool {
	return event.Has(fsnotify.Write)
}

func isCreate(event fsnotify.Event) bool {
	return event.Has(fsnotify.Create)
}

func isRemove(event fsnotify.Event) bool {
	return event.Has(fsnotify.Remove)
}

// concatCerts concatenates PEM certificates, making sure each one starts on a new line
func concatCerts(certsPEM []string) []byte {
	if len(certsPEM) == 0 {
		return []byte{}
	}
	var certChain bytes.Buffer
	for i, c := range certsPEM {
		certChain.WriteString(c)
		if i < len(certsPEM)-1 && !strings.HasSuffix(c, "\n") {
			certChain.WriteString("\n")
		}
	}
	return certChain.Bytes()
}

// UpdateConfigTrustBundle : Update the Configured Trust Bundle in the secret Manager client
func (sc *SecretManagerClient) UpdateConfigTrustBundle(trustBundle []byte) error {
	sc.configTrustBundleMutex.Lock()
	if bytes.Equal(sc.configTrustBundle, trustBundle) {
		cacheLog.Debugf("skip for same trust bundle")
		sc.configTrustBundleMutex.Unlock()
		return nil
	}
	sc.configTrustBundle = trustBundle
	sc.configTrustBundleMutex.Unlock()
	cacheLog.Debugf("update new trust bundle")
	sc.OnSecretUpdate(security.RootCertReqResourceName)
	sc.cache.SetWorkload(nil)
	sc.OnSecretUpdate(security.WorkloadKeyCertResourceName)
	return nil
}

// mergeTrustAnchorBytes: Merge cert bytes with the cached TrustAnchors.
func (sc *SecretManagerClient) mergeTrustAnchorBytes(caCerts []byte) []byte {
	return sc.mergeConfigTrustBundle(pkiutil.PemCertBytestoString(caCerts))
}

// mergeConfigTrustBundle: merge rootCerts trustAnchors provided in args with proxyConfig trustAnchors
// ensure dedup and sorting before returning trustAnchors
func (sc *SecretManagerClient) mergeConfigTrustBundle(rootCerts []string) []byte {
	sc.configTrustBundleMutex.RLock()
	existingCerts := pkiutil.PemCertBytestoString(sc.configTrustBundle)
	sc.configTrustBundleMutex.RUnlock()
	anchors := sets.New[string]()
	for _, cert := range existingCerts {
		anchors.Insert(cert)
	}
	for _, cert := range rootCerts {
		anchors.Insert(cert)
	}
	anchorBytes := []byte{}
	for _, cert := range sets.SortedList(anchors) {
		anchorBytes = pkiutil.AppendCertByte(anchorBytes, []byte(cert))
	}
	return anchorBytes
}
