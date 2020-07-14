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
	"errors"
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"

	"istio.io/istio/pkg/testcerts"
	"istio.io/pkg/filewatcher"
)

var (
	certFile   = "foo.pem"
	keyFile    = "key.pem"
	caCertFile = "bar.pem"
)

func TestWatchRotation(t *testing.T) {
	var wgAddedWatch sync.WaitGroup

	addedWatchProbe := func(_ string, _ bool) { wgAddedWatch.Done() }
	var fakeWatcher *filewatcher.FakeWatcher
	newFileWatcher, fakeWatcher = filewatcher.NewFakeWatcher(addedWatchProbe)

	defer func() {
		newFileWatcher = filewatcher.NewWatcher
		readFile = ioutil.ReadFile
		watchCertEventHandledProbe = nil
		watchKeyEventHandledProbe = nil
	}()

	readFile = func(filename string) ([]byte, error) {
		switch filename {
		case certFile:
			return testcerts.ServerCert, nil
		case keyFile:
			return testcerts.ServerKey, nil
		case caCertFile:
			return testcerts.CACert, nil
		}
		return nil, &os.PathError{Op: "open", Path: filename, Err: os.ErrNotExist}
	}

	stopCh := make(chan struct{})
	defer close(stopCh)
	options := Options{
		CertificateFile:   certFile,
		KeyFile:           keyFile,
		CACertificateFile: caCertFile,
	}

	// We expect two files to be watched (key+cert). Wait for the watch
	// to be added before proceeding.
	wgAddedWatch.Add(2)
	w, err := WatchFiles(stopCh, &options)
	if err != nil {
		t.Fatalf("Initial WatchFiles() failed: %v", err)
	}
	watcher := w.(*notifyWatcher)
	wgAddedWatch.Wait()

	var wgCertEvent sync.WaitGroup
	var wgKeyEvent sync.WaitGroup

	watchCertEventHandledProbe = func() { wgCertEvent.Done() }
	watchKeyEventHandledProbe = func() { wgKeyEvent.Done() }

	// verify cert and key can be reloaded via watcher interface
	readFile = func(filename string) ([]byte, error) {
		switch filename {
		case certFile:
			return testcerts.RotatedCert, nil
		case keyFile:
			return testcerts.RotatedKey, nil
		case caCertFile:
			return testcerts.CACert, nil
		default:
			return nil, &os.PathError{Op: "open", Path: filename, Err: os.ErrNotExist}
		}
	}
	wgCertEvent.Add(1)
	wgKeyEvent.Add(1)
	fakeWatcher.InjectEvent(certFile, fsnotify.Event{
		Name: certFile,
		Op:   fsnotify.Write,
	})
	fakeWatcher.InjectEvent(keyFile, fsnotify.Event{
		Name: keyFile,
		Op:   fsnotify.Write,
	})
	wgCertEvent.Wait()
	wgKeyEvent.Wait()
	want, _ := tls.X509KeyPair(testcerts.RotatedCert, testcerts.RotatedKey)
	if !reflect.DeepEqual(watcher.Get(), want) {
		t.Fatalf("wrong rotated certificate: \ngot %v \nwant %v", watcher.cert, want)
	}

	// generate fake errors to increase code coverage
	fakeWatcher.InjectError(certFile, errors.New("fakeCertWatcher error to increase code coverage of error event path")) // nolint: lll
	fakeWatcher.InjectError(keyFile, errors.New("fakeCertWatcher error to increase code coverage of error event path"))  // nonlint: lll

	// verify the previously loaded cert/key are retained if the updated files cannot be loaded.
	readFile = func(filename string) ([]byte, error) {
		return nil, &os.PathError{Op: "open", Path: filename, Err: os.ErrNotExist}
	}
	wgCertEvent.Add(1)
	wgKeyEvent.Add(1)
	fakeWatcher.InjectEvent(certFile, fsnotify.Event{
		Name: certFile,
		Op:   fsnotify.Write,
	})
	fakeWatcher.InjectEvent(keyFile, fsnotify.Event{
		Name: keyFile,
		Op:   fsnotify.Write,
	})
	wgCertEvent.Wait()
	wgKeyEvent.Wait()

	want, _ = tls.X509KeyPair(testcerts.RotatedCert, testcerts.RotatedKey)
	if !reflect.DeepEqual(watcher.cert, want) {
		t.Fatalf("wrong rotated certificate: \ngot %v \nwant %v", watcher.cert, want)
	}
}

func TestWatchFiles(t *testing.T) {
	var testCases = []struct {
		name   string
		key    []byte
		cert   []byte
		cacert []byte
		rotate bool
		err    string
	}{
		{
			name:   "basic",
			key:    testcerts.ServerKey,
			cert:   testcerts.ServerCert,
			cacert: testcerts.CACert,
		},
		{
			name:   "badcert",
			key:    testcerts.ServerKey,
			cert:   testcerts.BadCert,
			cacert: testcerts.CACert,
			err:    "error loading client certificate files",
		},
		{
			name:   "badcacert",
			key:    testcerts.ServerKey,
			cert:   testcerts.ServerCert,
			cacert: testcerts.BadCert,
			err:    "failed to append loaded CA certificate",
		},
		{
			name:   "noclientcert",
			key:    testcerts.ServerKey,
			cacert: testcerts.CACert,
			err:    "open foo.pem: file does not exist",
		},
		{
			name:   "noclientkey",
			cert:   testcerts.ServerCert,
			cacert: testcerts.CACert,
			err:    "open key.pem: file does not exist",
		},
		{
			name: "nocacert",
			key:  testcerts.ServerKey,
			cert: testcerts.ServerCert,
			err:  "error loading CA certificate file",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			newFileWatcher, _ = filewatcher.NewFakeWatcher(nil)
			defer func() {
				newFileWatcher = filewatcher.NewWatcher
				readFile = ioutil.ReadFile
				watchCertEventHandledProbe = nil
				watchKeyEventHandledProbe = nil
			}()

			readFile = func(filename string) ([]byte, error) {
				switch filename {
				case certFile:
					if testCase.cert != nil {
						return testCase.cert, nil
					}
				case keyFile:
					if testCase.key != nil {
						return testCase.key, nil
					}
				case caCertFile:
					if testCase.cacert != nil {
						return testCase.cacert, nil
					}
				}
				return nil, &os.PathError{Op: "open", Path: filename, Err: os.ErrNotExist}
			}

			stopCh := make(chan struct{})
			defer close(stopCh)
			options := Options{
				CertificateFile:   certFile,
				KeyFile:           keyFile,
				CACertificateFile: caCertFile,
			}

			_, err := WatchFiles(stopCh, &options)
			if testCase.err != "" && err == nil {
				t.Fatalf("Expected error not found: %v", testCase.err)
			}

			if testCase.err == "" && err != nil {
				t.Fatalf("Unexpected error found: %v", err)
			}

			if err != nil && !strings.HasPrefix(err.Error(), testCase.err) {
				t.Fatalf("Error mismatch: got:%v, wanted:%q", err, testCase.err)
			}
		})
	}
}

func TestWatchFolder(t *testing.T) {
	dir, err := ioutil.TempDir(os.TempDir(), "TestLoadFromFolder")
	if err != nil {
		t.Fatalf("Error creating temp dir: %v", err)
	}

	certFile := path.Join(dir, "cert-chain.pem")
	keyFile := path.Join(dir, "key.pem")
	caCertFile := path.Join(dir, "root-cert.pem")

	if err = ioutil.WriteFile(certFile, testcerts.ServerCert, os.ModePerm); err != nil {
		t.Fatalf("Error writing cert file: %v", err)
	}
	if err = ioutil.WriteFile(keyFile, testcerts.ServerKey, os.ModePerm); err != nil {
		t.Fatalf("Error writing key file: %v", err)
	}
	if err = ioutil.WriteFile(caCertFile, testcerts.CACert, os.ModePerm); err != nil {
		t.Fatalf("Error writing CA cert file: %v", err)
	}

	stopCh := make(chan struct{})
	defer close(stopCh)
	_, err = WatchFolder(stopCh, dir)
	if err != nil {
		t.Fatalf("Unexpected error found: %v", err)
	}
}

func TestAttachCobraFlags(t *testing.T) {
	o := DefaultOptions()
	cmd := &cobra.Command{}
	o.AttachCobraFlags(cmd)

	cases := []struct {
		name      string
		wantValue string
	}{
		{
			name:      "certFile",
			wantValue: o.CertificateFile,
		},
		{
			name:      "keyFile",
			wantValue: o.KeyFile,
		},
		{
			name:      "caCertFile",
			wantValue: o.CACertificateFile,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(tt *testing.T) {
			got := cmd.Flag(c.name)
			if got == nil {
				tt.Fatal("flag not found")
			}
			if got.Value.String() != c.wantValue {
				tt.Fatalf("wrong default value: got %q want %q", got.Value.String(), c.wantValue)
			}
		})
	}
}
