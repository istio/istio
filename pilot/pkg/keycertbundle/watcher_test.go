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
	"bytes"
	"os"
	"path"
	"testing"
)

func TestWatcher(t *testing.T) {
	watcher := NewWatcher()

	// 1. no key cert bundle
	_, watch1 := watcher.AddWatcher()
	select {
	case bundle := <-watch1:
		t.Errorf("watched unexpected keyCertBundle: %v", bundle)
		return
	default:
	}

	key := []byte("key")
	cert := []byte("cert")
	ca := []byte("caBundle")
	crl := []byte("crl")
	// 2. set key cert bundle
	watcher.SetAndNotify(key, cert, ca)
	watcher.SetAndNotifyCACRL(crl)
	select {
	case <-watch1:
		keyCertBundle := watcher.GetKeyCertBundle()
		if !bytes.Equal(keyCertBundle.KeyPem, key) || !bytes.Equal(keyCertBundle.CertPem, cert) ||
			!bytes.Equal(keyCertBundle.CABundle, ca) || !bytes.Equal(keyCertBundle.CRL, crl) {
			t.Errorf("got wrong keyCertBundle %v", keyCertBundle)
		}
	default:
		t.Errorf("watched non keyCertBundle")
	}

	// 3. set new key cert bundle, notify all watchers
	_, watch2 := watcher.AddWatcher()

	key = []byte("key2")
	cert = []byte("cert2")
	ca = []byte("caBundle2")
	crl = []byte("crl2")
	watcher.SetAndNotify(key, cert, ca)
	watcher.SetAndNotifyCACRL(crl)
	select {
	case <-watch1:
		keyCertBundle := watcher.GetKeyCertBundle()
		if !bytes.Equal(keyCertBundle.KeyPem, key) || !bytes.Equal(keyCertBundle.CertPem, cert) ||
			!bytes.Equal(keyCertBundle.CABundle, ca) || !bytes.Equal(keyCertBundle.CRL, crl) {
			t.Errorf("got wrong keyCertBundle %v", keyCertBundle)
		}
	default:
		t.Errorf("watcher1 watched non keyCertBundle")
	}
	select {
	case <-watch2:
		keyCertBundle := watcher.GetKeyCertBundle()
		if !bytes.Equal(keyCertBundle.KeyPem, key) || !bytes.Equal(keyCertBundle.CertPem, cert) ||
			!bytes.Equal(keyCertBundle.CABundle, ca) {
			t.Errorf("got wrong keyCertBundle %v", keyCertBundle)
		}
	default:
		t.Errorf("watcher2 watched non keyCertBundle")
	}
}

func TestWatcherFromFile(t *testing.T) {
	watcher := NewWatcher()

	// 1. no key cert bundle
	_, watch1 := watcher.AddWatcher()
	select {
	case bundle := <-watch1:
		t.Errorf("watched unexpected keyCertBundle: %v", bundle)
		return
	default:
	}

	tmpDir := t.TempDir()

	key := []byte("key")
	cert := []byte("cert")
	ca := []byte("caBundle")
	keyFile := path.Join(tmpDir, "key.pem")
	certFile := path.Join(tmpDir, "cert.pem")
	caFile := path.Join(tmpDir, "ca.pem")

	os.WriteFile(keyFile, key, os.ModePerm)
	os.WriteFile(certFile, cert, os.ModePerm)
	os.WriteFile(caFile, ca, os.ModePerm)

	// 2. set key cert bundle
	watcher.SetFromFilesAndNotify(keyFile, certFile, caFile)
	select {
	case <-watch1:
		keyCertBundle := watcher.GetKeyCertBundle()
		if !bytes.Equal(keyCertBundle.KeyPem, key) || !bytes.Equal(keyCertBundle.CertPem, cert) ||
			!bytes.Equal(keyCertBundle.CABundle, ca) {
			t.Errorf("got wrong keyCertBundle %v", keyCertBundle)
		}
	default:
		t.Errorf("watched non keyCertBundle")
	}
}
