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
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/pkg/errors"

	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/testcerts"
)

func TestPollingWatcher_Basic(t *testing.T) {
	f := newFixture(t)
	defer f.cleanup()

	if err := f.newWatcher(); err != nil {
		t.Fatalf("error starting watch: %v", err)
	}

	want, _ := tls.X509KeyPair(testcerts.ServerCert, testcerts.ServerKey)
	if !reflect.DeepEqual(f.w.Get(), want) {
		t.Fatalf("wrong certificate: \ngot %v \nwant %v", f.w.Get(), want)
	}

	pool := f.w.certPool()
	if pool != f.w.(*pollingWatcher).caCertPool {
		t.Fatal("wrong pool returned from certPool()")
	}
}

func TestPollingWatcher_Initial_MissingCaCert(t *testing.T) {
	f := newFixture(t)
	defer f.cleanup()

	deleteFile(t, f.caCertFile)

	err := f.newWatcher()
	if err == nil {
		t.Fatalf("expected error not found: %v", err)
	}

	expected := "error loading CA certificate file"
	if !strings.HasPrefix(err.Error(), expected) {
		t.Fatalf("Mismatch. got=%q, wanted=%q", err.Error(), expected)
	}
}

func TestPollingWatcher_Initial_MissingKey(t *testing.T) {
	f := newFixture(t)
	defer f.cleanup()

	deleteFile(t, f.keyFile)

	err := f.newWatcher()
	if err == nil {
		t.Fatalf("expected error not found: %v", err)
	}

	expected := "unable to read key file"
	if !strings.HasPrefix(err.Error(), expected) {
		t.Fatalf("Mismatch. got=%q, wanted=%q", err.Error(), expected)
	}
}

func TestPollingWatcher_Initial_MissingCert(t *testing.T) {
	f := newFixture(t)
	defer f.cleanup()

	deleteFile(t, f.certFile)

	err := f.newWatcher()
	if err == nil {
		t.Fatalf("expected error not found: %v", err)
	}

	expected := "unable to read cert file"
	if !strings.HasPrefix(err.Error(), expected) {
		t.Fatalf("Mismatch. got=%q, wanted=%q", err.Error(), expected)
	}
}

func TestPollingWatcher_Initial_BogusCaCert(t *testing.T) {
	f := newFixture(t)
	defer f.cleanup()

	writeFile(t, f.caCertFile, []byte{0xDE, 0xAD, 0xBE, 0xEF})

	err := f.newWatcher()
	if err == nil {
		t.Fatalf("expected error not found: %v", err)
	}

	expected := "failed to append loaded CA certificate to the certificate pool"
	if !strings.HasPrefix(err.Error(), expected) {
		t.Fatalf("Mismatch. got=%q, wanted=%q", err.Error(), expected)
	}
}

func TestPollingWatcher_Initial_BogusCert(t *testing.T) {
	f := newFixture(t)
	defer f.cleanup()

	writeFile(t, f.certFile, []byte{0xDE, 0xAD, 0xBE, 0xEF})

	err := f.newWatcher()
	if err == nil {
		t.Fatalf("expected error not found: %v", err)
	}

	expected := "unable load cert files as pair: error loading client certificate files"
	if !strings.HasPrefix(err.Error(), expected) {
		t.Fatalf("Mismatch. got=%q, wanted=%q", err.Error(), expected)
	}
}

func TestPollingWatcher_Initial_BogusKey(t *testing.T) {
	f := newFixture(t)
	defer f.cleanup()

	writeFile(t, f.keyFile, []byte{0xDE, 0xAD, 0xBE, 0xEF})

	err := f.newWatcher()
	if err == nil {
		t.Fatalf("expected error not found: %v", err)
	}

	expected := "unable load cert files as pair: error loading client certificate files"
	if !strings.HasPrefix(err.Error(), expected) {
		t.Fatalf("Mismatch. got=%q, wanted=%q", err.Error(), expected)
	}
}

func TestPollingWatcher_PollingUpdates_Update(t *testing.T) {
	f := newFixture(t)
	defer f.cleanup()

	if err := f.newWatcher(); err != nil {
		t.Fatalf("error starting watch: %v", err)
	}

	if err := f.getPollError(); err != nil {
		t.Fatalf("unexpected initial state poll error: %v", err)
	}

	want, _ := tls.X509KeyPair(testcerts.ServerCert, testcerts.ServerKey)
	if !reflect.DeepEqual(f.w.Get(), want) {
		t.Fatalf("wrong initial certificate: \ngot %v \nwant %v", f.w.Get(), want)
	}

	writeFile(t, f.certFile, testcerts.RotatedCert)
	_ = f.waitForPollError(t) // This allows us to ensure synchronization.
	writeFile(t, f.keyFile, testcerts.RotatedKey)
	f.waitForNoPollError(t)

	want, _ = tls.X509KeyPair(testcerts.RotatedCert, testcerts.RotatedKey)
	if !reflect.DeepEqual(f.w.Get(), want) {
		t.Fatalf("wrong rotated certificate: \ngot %v \nwant %v", f.w.Get(), want)
	}
}

func TestPollingWatcher_PollingUpdates_MissingCertFile(t *testing.T) {
	f := newFixture(t)
	defer f.cleanup()

	if err := f.newWatcher(); err != nil {
		t.Fatalf("error starting watch: %v", err)
	}

	if err := f.getPollError(); err != nil {
		t.Fatalf("unexpected initial state poll error: %v", err)
	}

	deleteFile(t, f.certFile)

	expected := "unable to read cert file"
	actual := f.waitForPollError(t)
	if !strings.HasPrefix(actual.Error(), expected) {
		t.Fatalf("Mismatch. got:%v, wanted:%v", actual, expected)
	}

	// Check for recovery
	writeFile(t, f.certFile, testcerts.ServerCert)
	f.waitForNoPollError(t)
}

func TestPollingWatcher_PollingUpdates_MissingKeyFile(t *testing.T) {
	f := newFixture(t)
	defer f.cleanup()

	if err := f.newWatcher(); err != nil {
		t.Fatalf("error starting watch: %v", err)
	}

	if err := f.getPollError(); err != nil {
		t.Fatalf("unexpected initial state poll error: %v", err)
	}

	deleteFile(t, f.keyFile)

	expected := "unable to read key file"
	actual := f.waitForPollError(t)
	if !strings.HasPrefix(actual.Error(), expected) {
		t.Fatalf("Mismatch. got:%v, wanted:%v", actual, expected)
	}

	// Check for recovery
	writeFile(t, f.keyFile, testcerts.ServerKey)
	f.waitForNoPollError(t)
}

func TestPollingWatcher_PollingUpdates_BogusCertFile(t *testing.T) {
	f := newFixture(t)
	defer f.cleanup()

	if err := f.newWatcher(); err != nil {
		t.Fatalf("error starting watch: %v", err)
	}

	if err := f.getPollError(); err != nil {
		t.Fatalf("unexpected initial state poll error: %v", err)
	}

	writeFile(t, f.certFile, []byte{0xDE, 0xAD, 0xBE, 0xEF})

	expected := "unable load cert files as pair"
	actual := f.waitForPollError(t)
	if !strings.HasPrefix(actual.Error(), expected) {
		t.Fatalf("Mismatch. got:%v, wanted:%v", actual, expected)
	}

	// Check for recovery
	writeFile(t, f.certFile, testcerts.ServerCert)
	f.waitForNoPollError(t)
}

func TestPollingWatcher_PollingUpdates_BogusKeyFile(t *testing.T) {
	f := newFixture(t)
	defer f.cleanup()

	if err := f.newWatcher(); err != nil {
		t.Fatalf("error starting watch: %v", err)
	}

	if err := f.getPollError(); err != nil {
		t.Fatalf("unexpected initial state poll error: %v", err)
	}

	writeFile(t, f.keyFile, []byte{0xDE, 0xAD, 0xBE, 0xEF})

	expected := "unable load cert files as pair"
	actual := f.waitForPollError(t)
	if !strings.HasPrefix(actual.Error(), expected) {
		t.Fatalf("Mismatch. got:%v, wanted:%v", actual, expected)
	}

	// Check for recovery
	writeFile(t, f.keyFile, testcerts.ServerKey)
	f.waitForNoPollError(t)
}

func TestPollingWatcher_Symlinks(t *testing.T) {
	f := newFixture(t)
	defer f.cleanup()

	// create actual files to symlink to
	actualCertFile := f.newTmpFile(t)
	actualKeyFile := f.newTmpFile(t)
	actualCaCertFile := f.newTmpFile(t)

	moveFile(t, f.keyFile, actualKeyFile)
	moveFile(t, f.certFile, actualCertFile)
	moveFile(t, f.caCertFile, actualCaCertFile)

	writeSymlink(t, f.certFile, actualCertFile)
	writeSymlink(t, f.keyFile, actualKeyFile)
	writeSymlink(t, f.caCertFile, actualCaCertFile)

	if err := f.newWatcher(); err != nil {
		t.Fatalf("error starting watch: %v", err)
	}

	want, _ := tls.X509KeyPair(testcerts.ServerCert, testcerts.ServerKey)
	if !reflect.DeepEqual(f.w.Get(), want) {
		t.Fatalf("wrong certificate: \ngot %v \nwant %v", f.w.Get(), want)
	}

	// Check for reload of certs via symlink

	updatedCertFile := f.newTmpFile(t)
	updatedKeyFile := f.newTmpFile(t)

	writeFile(t, updatedKeyFile, testcerts.RotatedKey)
	writeFile(t, updatedCertFile, testcerts.RotatedCert)

	deleteFile(t, f.keyFile)
	deleteFile(t, f.certFile)
	writeSymlink(t, f.keyFile, updatedKeyFile)
	writeSymlink(t, f.certFile, updatedCertFile)

	// wait until cert is updated
	_, err := retry.Do(func() (interface{}, bool, error) {
		want, _ := tls.X509KeyPair(testcerts.RotatedCert, testcerts.RotatedKey)
		if !reflect.DeepEqual(f.w.Get(), want) {
			return nil, false, fmt.Errorf("expected certificate not found:\ngot %v \nwant %v", f.w.Get(), want)
		}

		return nil, true, nil
	})

	if err != nil {
		t.Fatalf("Error getting updated cert: %v", err)
	}
}

type watcherFixture struct {
	w  CertificateWatcher
	ch chan struct{}

	folder     string
	certFile   string
	caCertFile string
	keyFile    string
}

func newFixture(t *testing.T) *watcherFixture {
	t.Helper()

	folder, err := ioutil.TempDir(os.TempDir(), "test_pollingwatcher")
	if err != nil {
		t.Fatalf("Error creating fixture: %v", err)
	}

	f := &watcherFixture{
		folder:     folder,
		keyFile:    path.Join(folder, defaultKeyFile),
		certFile:   path.Join(folder, defaultCertificateFile),
		caCertFile: path.Join(folder, defaultCACertificateFile),
	}

	writeFile(t, f.certFile, testcerts.ServerCert)
	writeFile(t, f.keyFile, testcerts.ServerKey)
	writeFile(t, f.caCertFile, testcerts.CACert)

	return f
}

func (f *watcherFixture) newWatcher() (err error) {
	ch := make(chan struct{})
	// Reduce poll time for quick turnaround of events
	w, err := pollFolder(ch, f.folder, time.Millisecond)
	if err != nil {
		return err
	}

	f.ch = ch
	f.w = w

	return nil
}

func (f *watcherFixture) getPollError() error {
	return f.w.(*pollingWatcher).pollError()
}

func (f *watcherFixture) waitForPollError(t *testing.T) (pollErr error) {
	t.Helper()

	_, err := retry.Do(func() (interface{}, bool, error) {
		pollErr = f.getPollError()
		if pollErr == nil {
			return nil, false, errors.New("poll error not found")
		}
		return nil, true, nil
	})

	if err != nil {
		t.Fatal(err)
	}
	return
}

func (f *watcherFixture) waitForNoPollError(t *testing.T) {
	t.Helper()

	_, err := retry.Do(func() (interface{}, bool, error) {
		pollErr := f.getPollError()
		if pollErr != nil {
			return nil, false, fmt.Errorf("poll error found: %v", pollErr)
		}
		return nil, true, nil
	})

	if err != nil {
		t.Fatal(err)
	}
}

func (f *watcherFixture) newTmpFile(t *testing.T) string {
	t.Helper()

	fi, err := ioutil.TempFile(f.folder, "")
	if err != nil {
		t.Fatalf("Error creating temp file: %v", err)
	}

	// Make sure the file doesn't exist
	if err = os.Remove(fi.Name()); err != nil {
		t.Fatalf("error deleting the just-created ttemp file: %v", err)
	}

	return fi.Name()
}

func (f *watcherFixture) cleanup() {
	if f.ch != nil {
		close(f.ch)
		f.ch = nil
	}
}

func writeFile(t *testing.T, filename string, contents []byte) {
	t.Helper()

	err := ioutil.WriteFile(filename, contents, os.ModePerm)
	if err != nil {
		t.Fatalf("Error writing file(%q): %v", filename, err)
	}
}

func deleteFile(t *testing.T, filename string) {
	t.Helper()

	err := os.Remove(filename)
	if err != nil {
		t.Fatalf("Error deleting file(%q): %v", filename, err)
	}
}

func moveFile(t *testing.T, source, destination string) {
	t.Helper()

	if err := os.Rename(source, destination); err != nil {
		t.Fatalf("Error moving file: %v", err)
	}
}

func writeSymlink(t *testing.T, linkfile string, sourcefile string) {
	t.Helper()

	err := os.Symlink(sourcefile, linkfile)
	if err != nil {
		t.Fatalf("error creating symlink file: %v", err)
	}
}
