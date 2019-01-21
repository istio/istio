// Copyright 2018 Istio Authors
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

package server

import (
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/fsnotify/fsnotify"

	"istio.io/istio/pkg/filewatcher"
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

func TestWatchAccessList_Exists_ButRemoved(t *testing.T) {
	removed := make(chan string, 10)
	var fake *filewatcher.FakeWatcher
	newFileWatcher, fake = filewatcher.NewFakeWatcher(func(path string, _ bool) { removed <- path })
	defer func() {
		newFileWatcher = filewatcher.NewWatcher
		readFile = ioutil.ReadFile
		watchEventHandledProbe = nil
	}()

	// No Pilot
	initial := `
allowed:
    - spiffe://cluster.local/ns/istio-system/sa/istio-mixer-service-account
`

	file, stopCh, checker, err := setupWatchAccessList(t, initial)
	defer close(stopCh)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if err = os.Remove(file); err != nil {
		t.Fatalf("Unexpected error deleting file: %v", err)
	}

	// fake the watch `Write` event and wait for the event to be handled and the accesslist updated.
	watchEventHandled := make(chan struct{})
	watchEventHandledProbe = func() { close(watchEventHandled) }
	fake.InjectEvent(file, fsnotify.Event{
		Name: file,
		Op:   fsnotify.Remove,
	})
	<-watchEventHandled

	// Check Pilot
	if !checker.Allowed("spiffe://cluster.local/ns/istio-system/sa/istio-pilot-service-account") {
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
	checker, err := watchAccessList(stopCh, file)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// We expect the request to go through.
	if !checker.Allowed("spiffe://foo/bar/baz/goo") {
		t.Fatal("Expected spiffe id to be allowed.")
	}
}

func TestWatchAccessList_Initial_NotExists_ButAdded(t *testing.T) {
	added := make(chan string, 10)
	var fake *filewatcher.FakeWatcher
	newFileWatcher, fake = filewatcher.NewFakeWatcher(func(path string, _ bool) { added <- path })
	defer func() {
		newFileWatcher = filewatcher.NewWatcher
		readFile = ioutil.ReadFile
		watchEventHandledProbe = nil
	}()

	folder, err := ioutil.TempDir(os.TempDir(), "testWatchAccessList")
	file := path.Join(folder, "accesslist.yaml")

	if err != nil {
		t.Fatalf("error creating tmp folder: %v", err)
	}

	stopCh := make(chan struct{})
	defer close(stopCh)
	checker, err := watchAccessList(stopCh, file)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// We expect the request to go through.
	if !checker.Allowed("spiffe://foo/bar/baz/goo") {
		t.Fatal("Expected spiffe id to be allowed.")
	}

	updated := `
allowed:
    - spiffe://cluster.local/ns/istio-system/sa/istio-pilot-service-account
`

	// inject the updated file read into the watcher
	readFile = func(filename string) ([]byte, error) {
		if filename != file {
			t.Fatalf("read wrong filename: got %v want %v", filename, file)
		}
		return []byte(updated), nil
	}

	// fake the watch `Write` event and wait for the event to be handled and the accesslist updated.
	watchEventHandled := make(chan struct{})
	watchEventHandledProbe = func() { close(watchEventHandled) }
	fake.InjectEvent(file, fsnotify.Event{
		Name: file,
		Op:   fsnotify.Create,
	})
	<-watchEventHandled

	// We expect the request to go through.
	if checker.Allowed("spiffe://foo/bar/baz/goo") {
		t.Fatal("Expected spiffe id to be not allowed.")
	}
}

func TestWatchAccessList_Update(t *testing.T) {
	added := make(chan string, 10)
	var fake *filewatcher.FakeWatcher
	newFileWatcher, fake = filewatcher.NewFakeWatcher(func(path string, _ bool) { added <- path })
	defer func() {
		newFileWatcher = filewatcher.NewWatcher
		readFile = ioutil.ReadFile
		watchEventHandledProbe = nil
	}()

	initial := `
allowed:
    - spiffe://cluster.local/ns/istio-system/sa/istio-mixer-service-account
`

	file, stopCh, checker, err := setupWatchAccessList(t, initial)
	defer close(stopCh)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	gotAddedFile := <-added
	if gotAddedFile != file {
		t.Fatalf("access list watcher read the wrong file: got %v want %v", gotAddedFile, file)
	}

	updated := `
allowed:
    - spiffe://cluster.local/ns/istio-system/sa/istio-pilot-service-account
`

	// inject the updated file read into the watcher
	readFile = func(filename string) ([]byte, error) {
		if filename != file {
			t.Fatalf("read wrong filename: got %v want %v", filename, file)
		}
		return []byte(updated), nil
	}

	// fake the watch `Write` event and wait for the event to be handled and the accesslist updated.
	watchEventHandled := make(chan struct{})
	watchEventHandledProbe = func() { close(watchEventHandled) }
	fake.InjectEvent(file, fsnotify.Event{
		Name: file,
		Op:   fsnotify.Write,
	})
	<-watchEventHandled

	if !checker.Allowed("spiffe://cluster.local/ns/istio-system/sa/istio-pilot-service-account") {
		t.Fatal("Expected spiffe id to be allowed.")
	}
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
