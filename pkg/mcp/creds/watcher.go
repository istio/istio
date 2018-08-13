//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package creds

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io/ioutil"
	"path"
	"sync"

	"github.com/howeyc/fsnotify"

	"istio.io/istio/pkg/log"
)

var scope = log.RegisterScope("mcp-creds", "MCP Credential utilities", 0)

// CertificateWatcher watches a x509 cert/key file and loads it up in memory as needed.
type CertificateWatcher struct {
	caCertFile string
	certFile   string
	keyFile    string

	stopCh <-chan struct{}

	certMutex sync.Mutex
	cert      tls.Certificate

	// Even though CA cert is not being watched, this type is still responsible for holding on to it
	// to pass into one of the create methods.
	caCertPool *x509.CertPool
}

// WatchFolder loads certificates from the given folder. It expects the
// following files:
// cert-chain.pem, key.pem: Certificate/key files for the client/server on this side.
// root-cert.pem: certificate from the CA that will be used for validating peer's certificate.
//
// Internally WatchFolder will call WatchFiles.
func WatchFolder(stop <-chan struct{}, folder string) (*CertificateWatcher, error) {
	certFile := path.Join(folder, "cert-chain.pem")
	keyFile := path.Join(folder, "key.pem")
	caCertFile := path.Join(folder, "root-cert.pem")

	return WatchFiles(stop, certFile, keyFile, caCertFile)
}

// WatchFiles loads certificate & key files from the file system. The method will start a background
// go-routine and watch for credential file changes. Callers should pass the return result to one of the
// create functions to create a transport credentials that can dynamically use rotated certificates.
// The supplied stop channel can be used to stop the go-routine and the watch.
func WatchFiles(stopCh <-chan struct{}, certFile, keyFile, caCertFile string) (*CertificateWatcher, error) {
	w := &CertificateWatcher{
		caCertFile: caCertFile,
		certFile:   certFile,
		keyFile:    keyFile,
		stopCh:     stopCh,
	}

	if err := w.start(); err != nil {
		return nil, err
	}

	return w, nil
}

// start watching and stop when the stopCh is closed. Returns an error if the initial load of the certificate
// fails.
func (c *CertificateWatcher) start() error {
	// Load CA Cert file
	caCertPool, err := loadCACert(c.caCertFile)
	if err != nil {
		return err
	}

	cert, err := loadCertPair(c.certFile, c.keyFile)
	if err != nil {
		return err
	}
	c.set(&cert)

	// TODO: https://github.com/istio/istio/issues/7877
	// It looks like fsnotify watchers have problems due to following symlinks. This needs to be handled.
	//
	certFileWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	keyFileWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	if err = certFileWatcher.Watch(c.certFile); err != nil {
		return err
	}

	if err = keyFileWatcher.Watch(c.keyFile); err != nil {
		_ = certFileWatcher.Close()
		return err
	}

	scope.Debugf("Begin watching certificate files: %s, %s: ", c.certFile, c.keyFile)

	// Coordinate the goroutines for orderly shutdown
	var exitSignal sync.WaitGroup
	exitSignal.Add(1)
	exitSignal.Add(1)

	go c.watch(exitSignal, certFileWatcher, keyFileWatcher)
	// Watch error events in a separate g See:
	// https://github.com/fsnotify/fsnotify#faq
	go c.watchErrors(exitSignal, certFileWatcher, keyFileWatcher)

	go closeWatchers(exitSignal, certFileWatcher, keyFileWatcher)

	c.caCertPool = caCertPool

	return nil
}

func (c *CertificateWatcher) watch(
	exitSignal sync.WaitGroup, certFileWatcher, keyFileWatcher *fsnotify.Watcher) {

	defer exitSignal.Done()

	for {
		select {
		case e := <-certFileWatcher.Event:
			if e.IsCreate() || e.IsModify() {
				cert, err := loadCertPair(c.certFile, c.keyFile)
				if err != nil {
					scope.Errorf("error loading certificates after watch event: %v", err)
				}
				c.set(&cert)
			}
			break

		case e := <-keyFileWatcher.Event:
			if e.IsCreate() || e.IsModify() {
				cert, err := loadCertPair(c.certFile, c.keyFile)
				if err != nil {
					scope.Errorf("error loading certificates after watch event: %v", err)
				}
				c.set(&cert)
			}
			break

		case <-c.stopCh:
			scope.Debug("stopping watch of certificate file changes")
			return
		}
	}
}

func (c *CertificateWatcher) watchErrors(
	exitSignal sync.WaitGroup, certFileWatcher, keyFileWatcher *fsnotify.Watcher) {

	defer exitSignal.Done()

	for {
		select {
		case e := <-keyFileWatcher.Error:
			scope.Errorf("error event while watching key file: %v", e)
			break

		case e := <-certFileWatcher.Error:
			scope.Errorf("error event while watching cert file: %v", e)
			break

		case <-c.stopCh:
			scope.Debug("stopping watch of certificate file errors")
			return
		}
	}
}

func closeWatchers(exitSignal sync.WaitGroup, certFileWatcher, keyFileWatcher *fsnotify.Watcher) {
	exitSignal.Wait()
	_ = certFileWatcher.Close()
	_ = keyFileWatcher.Close()
}

// set the certificate directly
func (c *CertificateWatcher) set(cert *tls.Certificate) {
	c.certMutex.Lock()
	defer c.certMutex.Unlock()
	c.cert = *cert
}

// get the currently loaded certificate.
func (c *CertificateWatcher) get() tls.Certificate {
	c.certMutex.Lock()
	defer c.certMutex.Unlock()
	return c.cert
}

// loadCertPair from the given set of files.
func loadCertPair(certFile, keyFile string) (tls.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		err = fmt.Errorf("error loading client certificate files (%s, %s): %v", certFile, keyFile, err)
	}

	return cert, err
}

// loadCACert, create a certPool and return.
func loadCACert(caCertFile string) (*x509.CertPool, error) {
	certPool := x509.NewCertPool()
	ca, err := ioutil.ReadFile(caCertFile)
	if err != nil {
		err = fmt.Errorf("error loading CA certificate file (%s): %v", caCertFile, err)
		return nil, err
	}

	if ok := certPool.AppendCertsFromPEM(ca); !ok {
		err = errors.New("failed to append loaded CA certificate to the certificate pool")
		return nil, err
	}

	return certPool, nil
}
