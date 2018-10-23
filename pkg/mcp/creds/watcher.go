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
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"

	"istio.io/istio/pkg/log"
)

type fileWatcher interface {
	Add(path string) error
	Close() error
	Events() chan fsnotify.Event
	Errors() chan error
}

type fsNotifyWatcher struct {
	*fsnotify.Watcher
}

func (w *fsNotifyWatcher) Add(path string) error       { return w.Watcher.Add(path) }
func (w *fsNotifyWatcher) Close() error                { return w.Watcher.Close() }
func (w *fsNotifyWatcher) Events() chan fsnotify.Event { return w.Watcher.Events }
func (w *fsNotifyWatcher) Errors() chan error          { return w.Watcher.Errors }

func newFsnotifyWatcher() (fileWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &fsNotifyWatcher{Watcher: w}, nil
}

var (
	newCertFileWatcher = newFsnotifyWatcher
	newKeyFileWatcher  = newFsnotifyWatcher
	readFile           = ioutil.ReadFile

	watchCertEventHandledProbe func()
	watchKeyEventHandledProbe  func()
)

var scope = log.RegisterScope("mcp-creds", "MCP Credential utilities", 0)

const (
	// defaultCertDir is the default directory in which MCP options reside.
	defaultCertDir = "/etc/istio/certs/"
	// defaultCertificateFile is the default name to use for the certificate file.
	defaultCertificateFile = "cert-chain.pem"
	// defaultKeyFile is the default name to use for the key file.
	defaultKeyFile = "key.pem"
	// defaultCACertificateFile is the default name to use for the Certificate Authority's certificate file.
	defaultCACertificateFile = "root-cert.pem"
)

// CertificateWatcher watches a x509 cert/key file and loads it up in memory as needed.
type CertificateWatcher struct {
	options Options

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
	cred := &Options{
		CertificateFile:   path.Join(folder, defaultCertificateFile),
		KeyFile:           path.Join(folder, defaultKeyFile),
		CACertificateFile: path.Join(folder, defaultCACertificateFile),
	}
	return WatchFiles(stop, cred)
}

// Options defines the credential options required for MCP.
type Options struct {
	// CertificateFile to use for mTLS gRPC.
	CertificateFile string
	// KeyFile to use for mTLS gRPC.
	KeyFile string
	// CACertificateFile is the trusted root certificate authority's cert file.
	CACertificateFile string
}

// DefaultOptions returns default credential options.
func DefaultOptions() *Options {
	return &Options{
		CertificateFile:   filepath.Join(defaultCertDir, defaultCertificateFile),
		KeyFile:           filepath.Join(defaultCertDir, defaultKeyFile),
		CACertificateFile: filepath.Join(defaultCertDir, defaultCACertificateFile),
	}
}

// AttachCobraDeprecatedFlags attaches a set of Cobra flags to the given Cobra command.
//
// Cobra is the command-line processor that Istio uses. This command attaches
// the necessary set of flags to configure the MCP options.
func (c *Options) AttachCobraDeprecatedFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVarP(&c.CertificateFile, "certFile", "", c.CertificateFile,
		"The location of the certificate file for mutual TLS")
	cmd.PersistentFlags().MarkDeprecated("certFile", "Use --meshConfig instead, and specify in MeshConfig.ConfigSources[].TlsSettings")
	cmd.PersistentFlags().StringVarP(&c.KeyFile, "keyFile", "", c.KeyFile,
		"The location of the key file for mutual TLS")
	cmd.PersistentFlags().MarkDeprecated("keyFile", "Use --meshConfig instead, and specify in MeshConfig.ConfigSources[].TlsSettings")
	cmd.PersistentFlags().StringVarP(&c.CACertificateFile, "caCertFile", "", c.CACertificateFile,
		"The location of the certificate file for the root certificate authority")
	cmd.PersistentFlags().MarkDeprecated("caCertFile", "Use --meshConfig instead, and specify in MeshConfig.ConfigSources[].TlsSettings")
}

// WatchFiles loads certificate & key files from the file system. The method will start a background
// go-routine and watch for credential file changes. Callers should pass the return result to one of the
// create functions to create a transport options that can dynamically use rotated certificates.
// The supplied stop channel can be used to stop the go-routine and the watch.
func WatchFiles(stopCh <-chan struct{}, credentials *Options) (*CertificateWatcher, error) {
	w := &CertificateWatcher{
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
func (c *CertificateWatcher) start() error {
	// Load CA Cert file
	caCertPool, err := loadCACert(c.options.CACertificateFile)
	if err != nil {
		return err
	}

	cert, err := loadCertPair(c.options.CertificateFile, c.options.KeyFile)
	if err != nil {
		return err
	}
	c.set(&cert)

	// TODO: https://github.com/istio/istio/issues/7877
	// It looks like fsnotify watchers have problems due to following symlinks. This needs to be handled.
	//
	certFileWatcher, err := newCertFileWatcher()
	if err != nil {
		return err
	}

	keyFileWatcher, err := newKeyFileWatcher()
	if err != nil {
		return err
	}

	if err = certFileWatcher.Add(c.options.CertificateFile); err != nil {
		return err
	}

	if err = keyFileWatcher.Add(c.options.KeyFile); err != nil {
		_ = certFileWatcher.Close()
		return err
	}

	scope.Debugf("Begin watching certificate files: %s, %s: ",
		c.options.CertificateFile, c.options.KeyFile)

	// Coordinate the goroutines for orderly shutdown
	exitSignal := &sync.WaitGroup{}
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

func (c *CertificateWatcher) watch(exitSignal *sync.WaitGroup, certFileWatcher, keyFileWatcher fileWatcher) {

	defer exitSignal.Done()

	for {
		select {
		case e := <-certFileWatcher.Events():
			if e.Op&fsnotify.Write == fsnotify.Write {
				cert, err := loadCertPair(c.options.CertificateFile, c.options.KeyFile)
				if err != nil {
					scope.Errorf("error loading certificates after watch event: %v", err)
				} else {
					c.set(&cert)
				}
			}
			if watchCertEventHandledProbe != nil {
				watchCertEventHandledProbe()
			}
		case e := <-keyFileWatcher.Events():
			if e.Op&fsnotify.Write == fsnotify.Write {
				cert, err := loadCertPair(c.options.CertificateFile, c.options.KeyFile)
				if err != nil {
					scope.Errorf("error loading certificates after watch event: %v", err)
				} else {
					c.set(&cert)
				}
			}
			if watchKeyEventHandledProbe != nil {
				watchKeyEventHandledProbe()
			}
		case <-c.stopCh:
			scope.Debug("stopping watch of certificate file changes")
			return
		}
	}
}

func (c *CertificateWatcher) watchErrors(exitSignal *sync.WaitGroup, certFileWatcher, keyFileWatcher fileWatcher) {

	defer exitSignal.Done()

	for {
		select {
		case e := <-keyFileWatcher.Errors():
			scope.Errorf("error event while watching key file: %v", e)
		case e := <-certFileWatcher.Errors():
			scope.Errorf("error event while watching cert file: %v", e)
		case <-c.stopCh:
			scope.Debug("stopping watch of certificate file errors")
			return
		}
	}
}

func closeWatchers(exitSignal *sync.WaitGroup, certFileWatcher, keyFileWatcher fileWatcher) { // nolint: interfacer
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
	certPEMBlock, err := readFile(certFile)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEMBlock, err := readFile(keyFile)
	if err != nil {
		return tls.Certificate{}, err
	}
	cert, err := tls.X509KeyPair(certPEMBlock, keyPEMBlock)
	if err != nil {
		err = fmt.Errorf("error loading client certificate files (%s, %s): %v", certFile, keyFile, err)
	}
	return cert, err
}

// loadCACert, create a certPool and return.
func loadCACert(caCertFile string) (*x509.CertPool, error) {
	certPool := x509.NewCertPool()
	ca, err := readFile(caCertFile)
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
