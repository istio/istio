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

package cache

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"istio.io/istio/pkg/security"
)

func TestMountedRootCRLRotation(t *testing.T) {
	oldTimeout := totalTimeout
	totalTimeout = 100 * time.Millisecond
	t.Cleanup(func() { totalTimeout = oldTimeout })
	dir := t.TempDir()
	ca, err := os.ReadFile("testdata/root-cert.pem")
	require.NoError(t, err)
	crl, err := os.ReadFile("../../pki/testdata/crl/ca-crl.pem")
	require.NoError(t, err)
	caPath, crlPath := filepath.Join(dir, "ca.pem"), filepath.Join(dir, "crl.pem")
	require.NoError(t, os.WriteFile(caPath, ca, 0o600))
	require.NoError(t, os.WriteFile(crlPath, crl, 0o600))
	updates := make(chan string, 100)
	sc := createCache(t, nil, func(name string) { updates <- name }, security.Options{FileDebounceDuration: time.Millisecond})
	cfg := security.SdsCertificateConfig{CaCertificatePath: caPath, CRLPath: crlPath}
	resource := cfg.GetRootResourceName()
	otherPath := filepath.Join(dir, "other-crl.pem")
	require.NoError(t, os.WriteFile(otherPath, crl, 0o600))
	other := security.SdsCertificateConfig{CaCertificatePath: caPath, CRLPath: otherPath}.GetRootResourceName()
	for _, name := range []string{resource, other} {
		secret, err := sc.GenerateSecret(name)
		require.NoError(t, err)
		require.Equal(t, crl, secret.CRL)
	}
	waitUpdate := func(want string) {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case name := <-updates:
				if name == want {
					return
				}
			case <-deadline:
				t.Fatal("no file update for " + want)
			}
		}
	}
	drain := func() {
		for len(updates) > 0 {
			<-updates
		}
	}
	for i, atomic := range []bool{false, true, true} {
		drain()
		rotated := append(bytes.Clone(crl), bytes.Repeat([]byte("\n"), i+1)...)
		if atomic {
			tmp := crlPath + ".new"
			require.NoError(t, os.WriteFile(tmp, rotated, 0o600))
			require.NoError(t, os.Rename(tmp, crlPath))
		} else {
			require.NoError(t, os.WriteFile(crlPath, rotated, 0o600))
		}
		waitUpdate(resource)
		secret, err := sc.GenerateSecret(resource)
		require.NoError(t, err)
		require.Equal(t, rotated, secret.CRL)
	}
	bundle := append(bytes.Clone(crl), crl...)
	require.NoError(t, os.WriteFile(crlPath, bundle, 0o600))
	bundleSecret, err := sc.GenerateSecret(resource)
	require.NoError(t, err)
	require.Equal(t, bundle, bundleSecret.CRL)
	for _, invalid := range [][]byte{
		nil,
		[]byte("bad PEM"),
		[]byte("-----BEGIN X509 CRL-----\nYmFk\n-----END X509 CRL-----\n"),
		append(bytes.Clone(crl), []byte("trailing garbage")...),
		append([]byte("-----BEGIN X509 CRL-----\ninvalid\n"), crl...),
	} {
		require.NoError(t, os.WriteFile(crlPath, invalid, 0o600))
		secret, err := sc.GenerateSecret(resource)
		require.Error(t, err)
		require.Nil(t, secret)
	}
	drain()
	require.NoError(t, os.WriteFile(crlPath, crl, 0o600))
	waitUpdate(resource)
	secret, err := sc.GenerateSecret(resource)
	require.NoError(t, err)
	require.Equal(t, crl, secret.CRL)
	require.NoError(t, os.Remove(crlPath))
	secret, err = sc.GenerateSecret(resource)
	require.Error(t, err)
	require.Nil(t, secret)
	drain()
	require.NoError(t, os.WriteFile(crlPath, crl, 0o600))
	waitUpdate(resource)
	secret, err = sc.GenerateSecret(resource)
	require.NoError(t, err)
	require.Equal(t, crl, secret.CRL)
	drain()
	require.NoError(t, os.WriteFile(caPath, ca, 0o600))
	seen := map[string]bool{}
	deadline := time.After(5 * time.Second)
	for !seen[resource] || !seen[other] {
		select {
		case name := <-updates:
			seen[name] = true
		case <-deadline:
			t.Fatalf("shared CA update did not notify both resources: %v", seen)
		}
	}
	drain()
	movedDir := dir + ".moved"
	t.Cleanup(func() { _ = os.RemoveAll(movedDir) })
	require.NoError(t, os.Rename(dir, movedDir))
	require.Eventually(t, func() bool {
		sc.certMutex.RLock()
		defer sc.certMutex.RUnlock()
		return len(sc.fileCerts) == 0
	}, 5*time.Second, time.Millisecond)
	drain()
	require.NoError(t, os.Mkdir(dir, 0o700))
	require.NoError(t, os.WriteFile(caPath, ca, 0o600))
	require.NoError(t, os.WriteFile(crlPath, crl, 0o600))
	secret, err = sc.GenerateSecret(resource)
	require.NoError(t, err)
	require.Equal(t, crl, secret.CRL)
	drain()
	require.NoError(t, os.WriteFile(crlPath, bundle, 0o600))
	waitUpdate(resource)
}
