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
	"os"
	"path"
	"testing"

	. "github.com/onsi/gomega"
)

func TestWatcher(t *testing.T) {
	watcher := NewWatcher()

	// 1. no key cert bundle
	watch1 := watcher.AddWatcher()
	select {
	case bundle := <-watch1:
		t.Errorf("watched unexpected keyCertBundle: %v", bundle)
		return
	default:
	}

	key := []byte("key")
	cert := []byte("cert")
	ca := []byte("caBundle")
	// 2. set key cert bundle
	watcher.SetAndNotify(key, cert, ca)
	select {
	case keyCertBundle := <-watch1:
		if string(keyCertBundle.KeyPem) != string(key) || string(keyCertBundle.CertPem) != string(cert) ||
			string(keyCertBundle.CABundle) != string(ca) {
			t.Errorf("got wrong keyCertBundle %v", keyCertBundle)
		}
	default:
		t.Errorf("watched non keyCertBundle")
	}

	// 3. new watcher get notified immediately
	watch2 := watcher.AddWatcher()
	select {
	case keyCertBundle := <-watch2:
		if string(keyCertBundle.KeyPem) != string(key) || string(keyCertBundle.CertPem) != string(cert) ||
			string(keyCertBundle.CABundle) != string(ca) {
			t.Errorf("got wrong keyCertBundle %v", keyCertBundle)
		}
	default:
		t.Errorf("watched non keyCertBundle")
	}

	// 4. set new key cert bundle, notify all watchers
	key = []byte("key2")
	cert = []byte("cert2")
	ca = []byte("caBundle2")
	watcher.SetAndNotify(key, cert, ca)
	select {
	case keyCertBundle := <-watch1:
		if string(keyCertBundle.KeyPem) != string(key) || string(keyCertBundle.CertPem) != string(cert) ||
			string(keyCertBundle.CABundle) != string(ca) {
			t.Errorf("got wrong keyCertBundle %v", keyCertBundle)
		}
	default:
		t.Errorf("watcher1 watched non keyCertBundle")
	}
	select {
	case keyCertBundle := <-watch2:
		if string(keyCertBundle.KeyPem) != string(key) || string(keyCertBundle.CertPem) != string(cert) ||
			string(keyCertBundle.CABundle) != string(ca) {
			t.Errorf("got wrong keyCertBundle %v", keyCertBundle)
		}
	default:
		t.Errorf("watcher2 watched non keyCertBundle")
	}
}

func TestWatcherFromFile(t *testing.T) {
	g := NewWithT(t)
	watcher := NewWatcher()

	// 1. no key cert bundle
	watch1 := watcher.AddWatcher()
	select {
	case bundle := <-watch1:
		t.Errorf("watched unexpected keyCertBundle: %v", bundle)
		return
	default:
	}

	tmpDir, err := ioutil.TempDir(os.TempDir(), t.Name())
	g.Expect(err).To(BeNil())

	key := []byte("key")
	cert := []byte("cert")
	ca := []byte("caBundle")
	keyFile := path.Join(tmpDir, "key.pem")
	certFile := path.Join(tmpDir, "cert.pem")
	caFile := path.Join(tmpDir, "ca.pem")

	ioutil.WriteFile(keyFile, key, os.ModePerm)
	ioutil.WriteFile(certFile, cert, os.ModePerm)
	ioutil.WriteFile(caFile, ca, os.ModePerm)

	// 2. set key cert bundle
	watcher.SetFromFilesAndNotify(keyFile, certFile, caFile)
	select {
	case keyCertBundle := <-watch1:
		if string(keyCertBundle.KeyPem) != string(key) || string(keyCertBundle.CertPem) != string(cert) ||
			string(keyCertBundle.CABundle) != string(ca) {
			t.Errorf("got wrong keyCertBundle %v", keyCertBundle)
		}
	default:
		t.Errorf("watched non keyCertBundle")
	}

	// 3. new watcher get notified immediately
	watch2 := watcher.AddWatcher()
	select {
	case keyCertBundle := <-watch2:
		if string(keyCertBundle.KeyPem) != string(key) || string(keyCertBundle.CertPem) != string(cert) ||
			string(keyCertBundle.CABundle) != string(ca) {
			t.Errorf("got wrong keyCertBundle %v", keyCertBundle)
		}
	default:
		t.Errorf("watched non keyCertBundle")
	}
}
