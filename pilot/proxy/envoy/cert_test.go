// Copyright 2017 Istio Authors
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

package envoy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io/ioutil"
	"os"
	"path"
	"testing"
	"time"

	"istio.io/pilot/proxy"
)

func TestWatchCerts(t *testing.T) {
	name, err := ioutil.TempDir("testdata", "certs")
	if err != nil {
		t.Errorf("failed to create a temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(name); err != nil {
			t.Errorf("failed to remove temp dir: %v", err)
		}
	}()

	called := make(chan bool)
	callbackFunc := func() {
		called <- true
	}

	go watchCerts(context.Background(), name, callbackFunc)

	// sleep one second to make sure the watcher is set up before change is made
	time.Sleep(time.Second)

	// make a change to the watched dir
	if _, err := ioutil.TempFile(name, "test.file"); err != nil {
		t.Errorf("failed to create a temp file in testdata/certs: %v", err)
	}

	select {
	case <-called:
		// expected
	case <-time.After(time.Second):
		t.Errorf("The callback is not called within time limit " + time.Now().String())
	}
}

func TestGenerateCertHash(t *testing.T) {
	name, err := ioutil.TempDir("testdata", "certs")
	if err != nil {
		t.Errorf("failed to create a temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(name); err != nil {
			t.Errorf("failed to remove temp dir: %v", err)
		}
	}()

	h := sha256.New()
	authFiles := []string{proxy.CertChainFilename, proxy.KeyFilename, proxy.RootCertFilename}
	for _, file := range authFiles {
		content := []byte(file)
		if err := ioutil.WriteFile(path.Join(name, file), content, 0644); err != nil {
			t.Errorf("failed to write file %s (error %v)", file, err)
		}
		if _, err := h.Write(content); err != nil {
			t.Errorf("failed to write hash (error %v)", err)
		}
	}
	expectedHash := h.Sum(nil)

	h2 := sha256.New()
	generateCertHash(h2, name, authFiles)
	actualHash := h2.Sum(nil)
	if !bytes.Equal(actualHash, expectedHash) {
		t.Errorf("Actual hash value (%v) is different than the expected hash value (%v)", actualHash, expectedHash)
	}
}
