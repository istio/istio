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
	"io/ioutil"
	"sync"

	"go.uber.org/atomic"
)

// KeyCertBundle stores the cert, private key and root cert for istiod.
type KeyCertBundle struct {
	CertPem  []byte
	KeyPem   []byte
	CABundle []byte
}

type Watcher struct {
	// Indicated whether bundle has been set, it is used to invoke watcher for the first time.
	initDone atomic.Bool
	mutex    sync.Mutex
	bundle   KeyCertBundle
	watchers []chan KeyCertBundle
}

func NewWatcher() *Watcher {
	return &Watcher{}
}

// AddWatcher returns channel to receive the updated items.
func (w *Watcher) AddWatcher() chan KeyCertBundle {
	ch := make(chan KeyCertBundle, 1)
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if w.initDone.Load() {
		ch <- w.bundle
	}
	w.watchers = append(w.watchers, ch)
	return ch
}

// SetAndNotify sets the key cert and root cert and notify the watchers.
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
	w.initDone.Store(true)
	for _, ch := range w.watchers {
		ch <- w.bundle
	}
}

// SetFromFilesAndNotify sets the key cert and root cert from files and notify the watchers.
func (w *Watcher) SetFromFilesAndNotify(keyFile, certFile, rootCert string) error {
	cert, err := ioutil.ReadFile(certFile)
	if err != nil {
		return err
	}
	key, err := ioutil.ReadFile(keyFile)
	if err != nil {
		return err
	}
	caBundle, err := ioutil.ReadFile(rootCert)
	if err != nil {
		return err
	}
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
	w.initDone.Store(true)
	for _, ch := range w.watchers {
		ch <- w.bundle
	}
	return nil
}

// GetCABundle returns the CABundle.
func (w *Watcher) GetCABundle() []byte {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return w.bundle.CABundle
}
