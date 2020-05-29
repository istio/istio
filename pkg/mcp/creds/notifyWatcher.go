//  Copyright Istio Authors
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
	"io/ioutil"
	"path"
	"sync"

	"github.com/fsnotify/fsnotify"

	"istio.io/pkg/filewatcher"
)

var (
	newFileWatcher = filewatcher.NewWatcher
	readFile       = ioutil.ReadFile

	watchCertEventHandledProbe func()
	watchKeyEventHandledProbe  func()
)

type notifyWatcher struct {
	options Options

	stopCh <-chan struct{}

	certMutex sync.Mutex
	cert      tls.Certificate

	// Even though CA cert is not being watched, this type is still responsible for holding on to it
	// to pass into one of the create methods.
	caCertPool *x509.CertPool
}

var _ CertificateWatcher = &notifyWatcher{}

func (n *notifyWatcher) certPool() *x509.CertPool {
	return n.caCertPool
}

// WatchFolder loads certificates from the given folder. It expects the
// following files:
// cert-chain.pem, key.pem: Certificate/key files for the client/server on this side.
// root-cert.pem: certificate from the CA that will be used for validating peer's certificate.
//
// Internally WatchFolder will call WatchFiles.
func WatchFolder(stop <-chan struct{}, folder string) (CertificateWatcher, error) {
	cred := &Options{
		CertificateFile:   path.Join(folder, defaultCertificateFile),
		KeyFile:           path.Join(folder, defaultKeyFile),
		CACertificateFile: path.Join(folder, defaultCACertificateFile),
	}
	return WatchFiles(stop, cred)
}

// WatchFiles loads certificate & key files from the file system. The method will start a background
// go-routine and watch for credential file changes. Callers should pass the return result to one of the
// create functions to create a transport options that can dynamically use rotated certificates.
// The supplied stop channel can be used to stop the go-routine and the watch.
func WatchFiles(stopCh <-chan struct{}, credentials *Options) (CertificateWatcher, error) {
	w := &notifyWatcher{
		options: *credentials,
		stopCh:  stopCh,
	}

	if err := w.start(); err != nil {
		return nil, err
	}

	return w, nil
}

// start watching and stop when the stopCh is closed. Returns an error if the initial load of the certificate
// fails.
func (n *notifyWatcher) start() error {
	// Load CA Cert file
	caCertPool, err := loadCACert(n.options.CACertificateFile)
	if err != nil {
		return err
	}

	cert, err := loadCertPair(n.options.CertificateFile, n.options.KeyFile)
	if err != nil {
		return err
	}
	n.set(&cert)

	fw := newFileWatcher()
	if err := fw.Add(n.options.CertificateFile); err != nil {
		return err
	}
	if err := fw.Add(n.options.KeyFile); err != nil {
		return err
	}

	scope.Debugf("Begin watching certificate files: %s, %s: ",
		n.options.CertificateFile, n.options.KeyFile)

	go n.watch(fw)

	n.caCertPool = caCertPool

	return nil
}

func (n *notifyWatcher) watch(fw filewatcher.FileWatcher) {
	for {
		select {
		case e, more := <-fw.Events(n.options.CertificateFile):
			if !more {
				return
			}
			if e.Op&fsnotify.Write == fsnotify.Write || e.Op&fsnotify.Create == fsnotify.Create {
				cert, err := loadCertPair(n.options.CertificateFile, n.options.KeyFile)
				if err != nil {
					scope.Errorf("error loading certificates after watch event: %v", err)
				} else {
					n.set(&cert)
				}
			}
			if watchCertEventHandledProbe != nil {
				watchCertEventHandledProbe()
			}
		case e, more := <-fw.Events(n.options.KeyFile):
			if !more {
				return
			}
			if e.Op&fsnotify.Write == fsnotify.Write || e.Op&fsnotify.Create == fsnotify.Create {
				cert, err := loadCertPair(n.options.CertificateFile, n.options.KeyFile)
				if err != nil {
					scope.Errorf("error loading certificates after watch event: %v", err)
				} else {
					n.set(&cert)
				}
			}
			if watchKeyEventHandledProbe != nil {
				watchKeyEventHandledProbe()
			}
		case e := <-fw.Errors(n.options.CertificateFile):
			scope.Errorf("error event while watching cert file: %v", e)
		case e := <-fw.Errors(n.options.KeyFile):
			scope.Errorf("error event while watching key file: %v", e)
		case <-n.stopCh:
			fw.Close()
			scope.Debug("stopping watch of certificate file changes")
			return
		}
	}
}

// set the certificate directly
func (n *notifyWatcher) set(cert *tls.Certificate) {
	n.certMutex.Lock()
	defer n.certMutex.Unlock()
	n.cert = *cert
}

// Get the currently loaded certificate.
func (n *notifyWatcher) Get() tls.Certificate {
	n.certMutex.Lock()
	defer n.certMutex.Unlock()
	return n.cert
}
