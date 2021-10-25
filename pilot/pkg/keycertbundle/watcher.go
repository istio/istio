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
	initDone  atomic.Bool
	mutex     sync.Mutex
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
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// SetFromFilesAndNotify sets the key cert and root cert from files and notify the watchers.
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
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	return nil
}

// GetCABundle returns the CABundle.
func (w *Watcher) GetCABundle() []byte {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return w.bundle.CABundle
}

// GetCABundle returns the CABundle.
func (w *Watcher) GetKeyCertBundle() KeyCertBundle {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return w.bundle
}
