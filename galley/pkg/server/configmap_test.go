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

package server

import (
	"io/ioutil"
	"os"
	"path"
	"testing"
	"time"

	"istio.io/istio/pkg/mcp/server"
)

func TestWatchAccessList_Basic(t *testing.T) {
	initial := `
allowed:
    - spiffe://cluster.local/ns/istio-system/sa/istio-mixer-service-account
`

	_, stopCh, checker, err := setupWatchAccessList(t, initial)
	defer close(stopCh)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !checker.Allowed("spiffe://cluster.local/ns/istio-system/sa/istio-mixer-service-account") {
		t.Fatal("Expected spiffe id to be allowed.")
	}
}

func TestWatchAccessList_Initial_Unparseable(t *testing.T) {
	initial := `
332332
	rfjeritojoi
`

	_, stopCh, _, err := setupWatchAccessList(t, initial)
	defer close(stopCh)
	if err == nil {
		t.Fatal("Expected error not found")
	}
}

func TestWatchAccessList_Initial_NotExists(t *testing.T) {
	folder, err := ioutil.TempDir(os.TempDir(), "testWatchAccessList")
	file := path.Join(folder, "accesslist.yaml")

	if err != nil {
		t.Fatalf("error creating tmp folder: %v", err)
	}

	stopCh := make(chan struct{})
	defer close(stopCh)
	if _, err = watchAccessList(stopCh, file); err == nil {
		t.Fatalf("expected error not found")
	}
}

func TestWatchAccessList_Update(t *testing.T) {
	initial := `
allowed:
    - spiffe://cluster.local/ns/istio-system/sa/istio-mixer-service-account
`

	file, stopCh, checker, err := setupWatchAccessList(t, initial)
	defer close(stopCh)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	updated := `
allowed:
    - spiffe://cluster.local/ns/istio-system/sa/istio-pilot-service-account
`

	writeFile(t, file, updated)

	for i := 0; i < 100; i++ {
		if checker.Allowed("spiffe://cluster.local/ns/istio-system/sa/istio-pilot-service-account") {
			return
		}

		time.Sleep(time.Millisecond * 10)
	}

	t.Fatal("Expected spiffe id to be allowed.")
}

func setupWatchAccessList(t *testing.T, initialdata string) (string, chan struct{}, *server.ListAuthChecker, error) {
	folder, err := ioutil.TempDir(os.TempDir(), "testWatchAccessList")
	file := path.Join(folder, "accesslist.yaml")
	if err != nil {
		t.Fatalf("error creating tmp folder: %v", err)
	}

	writeFile(t, file, initialdata)

	stopCh := make(chan struct{})
	checker, err := watchAccessList(stopCh, file)
	return file, stopCh, checker, err
}

func writeFile(t *testing.T, file, contents string) {
	if err := ioutil.WriteFile(file, []byte(contents), os.ModePerm); err != nil {
		t.Fatalf("error writing access file contents: %v", err)
	}
}
