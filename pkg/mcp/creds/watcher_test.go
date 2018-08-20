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
	"io/ioutil"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"istio.io/istio/pkg/mcp/testing/testcerts"
)

func TestWatchFiles(t *testing.T) {
	var testCases = []struct {
		name   string
		key    []byte
		cert   []byte
		cacert []byte
		err    string
	}{
		{
			name:   "basic",
			key:    testcerts.ServerKey,
			cert:   testcerts.ServerCert,
			cacert: testcerts.CACert,
		},
		{
			name:   "rotated",
			key:    testcerts.RotatedKey,
			cert:   testcerts.RotatedCert,
			cacert: testcerts.RotatedCert,
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
			err:    "error loading client certificate files",
		},
		{
			name:   "noclientkey",
			cert:   testcerts.ServerCert,
			cacert: testcerts.CACert,
			err:    "error loading client certificate files",
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
			dir, err := ioutil.TempDir(os.TempDir(), testCase.name)
			if err != nil {
				t.Fatalf("Error creating temp dir: %v", err)
			}

			defer func() {
				_ = os.RemoveAll(dir)
			}()

			certFile := path.Join(dir, "foo.pem")
			keyFile := path.Join(dir, "key.pem")
			caCertFile := path.Join(dir, "bar.pem")

			if testCase.cert != nil {
				if err = ioutil.WriteFile(certFile, testCase.cert, os.ModePerm); err != nil {
					t.Fatalf("Error writing cert file: %v", err)
				}
			}

			if testCase.key != nil {
				if err = ioutil.WriteFile(keyFile, testCase.key, os.ModePerm); err != nil {
					t.Fatalf("Error writing key file: %v", err)
				}
			}

			if testCase.cacert != nil {
				if err = ioutil.WriteFile(caCertFile, testCase.cacert, os.ModePerm); err != nil {
					t.Fatalf("Error writing CA cert file: %v", err)
				}
			}

			stopCh := make(chan struct{})
			defer close(stopCh)
			options := Options{
				CertificateFile:   certFile,
				KeyFile:           keyFile,
				CACertificateFile: caCertFile,
			}
			_, err = WatchFiles(stopCh, &options)
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
