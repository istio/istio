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

package keycertbundle

import (
	"os"
	"sync"
)

// KeyCertBundle stores the cert, private key and root cert for istiod.
type KeyCertBundle struct {
	CertPem []byte
	KeyPem  []byte

	// CABundle is distributed to all namespaces by namespace controller (if enable), even if
	// KeyPem and CertPem are not set.
	CABundle []byte
}

type Watcher struct {
	mutex     sync.RWMutex
	bundle    KeyCertBundle
	watcherID int32
	watchers  map[int32]chan struct{}
}

func NewWatcher() *Watcher {
	return &Watcher{
		watchers: make(map[int32]chan struct{}),
	}
}

// AddWatcher returns channel to receive the updated items.
func (w *Watcher) AddWatcher() (int32, chan struct{}) {
	ch := make(chan struct{}, 1)
	w.mutex.Lock()
	defer w.mutex.Unlock()
	id := w.watcherID
	w.watchers[id] = ch
	w.watcherID++

	return id, ch
}

// RemoveWatcher removes the given watcher.
func (w *Watcher) RemoveWatcher(id int32) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	ch := w.watchers[id]
	if ch != nil {
		close(ch)
	}
	delete(w.watchers, id)
}

// SetAndNotify sets the key cert and root cert and notify the watchers.
// This is used either from /var/run/secrets/istiod files or by the certcontroller.
//
// key and cert may be nil - which may happen if no root CA is configured and no mounted certificates are found.
// In this case, Istiod should disable the TLS port - and use a gateway or waypoint as frontend.
// The caBundle will still be replicated to namespaces.
func (w *Watcher) SetAndNotify(key, cert, caBundle []byte) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if len(key) != 0 {
		w.bundle.KeyPem = key
	}
	if len(cert) != 0 {
		w.bundle.CertPem = cert
	}
	if len(caBundle) != 0 {
		w.bundle.CABundle = caBundle
	}
	for _, ch := range w.watchers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// SetFromFilesAndNotify sets the key cert and root cert from files and notify the watchers.
// This is a wrapper around SetAndNotify that reads the 3 files - not to confuse with the
// /etc/cacerts, this is used for Istiod having its own certs and roots set
// in /var/run/secrets/istiod/tls/ca.crt root /var/run/secrets/istiod/ca/root-cert.pem
func (w *Watcher) SetFromFilesAndNotify(keyFile, certFile, rootCert string) error {
	cert, err := os.ReadFile(certFile)
	if err != nil {
		return err
	}
	key, err := os.ReadFile(keyFile)
	if err != nil {
		return err
	}
	caBundle, err := os.ReadFile(rootCert)
	if err != nil {
		return err
	}
	w.SetAndNotify(key, cert, caBundle)
	return nil
}

// GetCABundle returns the CABundle.
func (w *Watcher) GetCABundle() []byte {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.bundle.CABundle
}

// GetKeyCertBundle returns the bundle.
func (w *Watcher) GetKeyCertBundle() KeyCertBundle {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.bundle
}
