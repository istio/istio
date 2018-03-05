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

package binder

import (
	"encoding/json"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/grpc"

	fvcreds "istio.io/istio/security/pkg/flexvolume"
)

func testWriteCredentials(path, file string, creds fvcreds.Credential) (string, error) {
	fileName := filepath.Join(path, file)
	var attrs []byte
	attrs, err := json.Marshal(creds)
	if err != nil {
		return "", err
	}
	err = ioutil.WriteFile(fileName, attrs, 0644)
	if err != nil {
		return "", err
	}
	return fileName, nil
}

func testWriteDefaultCredentials(path, UID string) string {
	credDir := filepath.Join(path, CredentialsSubdir)
	if err := os.MkdirAll(credDir, 0777); err != nil {
		panic(err)
	}

	expectedCred := fvcreds.Credential{UID: UID,
		Workload:       "foo",
		ServiceAccount: "sa",
		Namespace:      "default",
	}
	fileName, err := testWriteCredentials(credDir, UID+CredentialsExtension, expectedCred)
	if err != nil {
		panic(err)
	}
	return fileName
}

func getListener(path, file string) (net.Listener, error) {
	fileName := filepath.Join(path, file)
	return net.Listen("unix", fileName)
}

func TestSearchPath(t *testing.T) {
	dir, err := ioutil.TempDir("", "test-binder")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	binder := NewBinder(dir)
	got := binder.SearchPath()
	if got != dir {
		t.Errorf("Expected %s got %s", dir, got)
	}
}

func TestReadCredential(t *testing.T) {
	dir, err := ioutil.TempDir("", "test-binder")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	expectedCred := fvcreds.Credential{UID: "test",
		Workload:       "foo",
		ServiceAccount: "sa",
		Namespace:      "default",
	}
	file, e := testWriteCredentials(dir, "test.json", expectedCred)
	if e != nil {
		panic(e)
	}
	var gotCred Credentials
	gotErr := readCredentials(file, &gotCred)
	if gotErr != nil {
		t.Errorf("Expected no error got %s", gotErr.Error())
	}
	if gotCred.WorkloadCredentials != expectedCred {
		t.Errorf("Expected %+v got %+v", expectedCred, gotCred.WorkloadCredentials)
	}
}

func TestReadCredentialErrPath(t *testing.T) {
	dir, err := ioutil.TempDir("", "test-binder")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	var gotCred Credentials
	gotErr := readCredentials(filepath.Join(dir, "foo"), &gotCred)
	if gotErr == nil {
		t.Errorf("Expected an error got none")
	}
}

func TestReadCredentialErrContent(t *testing.T) {
	dir, err := ioutil.TempDir("", "test-binder")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	// create an empty credential file
	credFile := filepath.Join(dir, "foo")
	f, err := os.OpenFile(credFile, os.O_RDONLY|os.O_CREATE, 0666)
	if err != nil {
		panic(err)
	}
	f.Close()

	var gotCred Credentials
	gotErr := readCredentials(credFile, &gotCred)
	if gotErr == nil {
		t.Errorf("Expected an error got none")
	}
}

func TestAddListener(t *testing.T) {
	dir, err := ioutil.TempDir("", "test-binder")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	UID := "test"
	// Create the mount directory
	mountDir := filepath.Join(dir, MountSubdir, UID)
	if e := os.MkdirAll(mountDir, 0777); e != nil {
		panic(err)
	}

	testWriteDefaultCredentials(dir, UID)

	ws := newWorkloadStore()
	b := &binder{
		searchPath: dir,
		server:     grpc.NewServer(grpc.Creds(ws)),
		workloads:  ws}

	gotErr := b.addListener(UID)
	if gotErr != nil {
		t.Errorf("Expected no error got %s", gotErr.Error())
	}
	// check if the socketfile is there
	socketFile := filepath.Join(mountDir, SocketFilename)
	stat, err := os.Stat(socketFile)
	if err != nil {
		t.Errorf("Expected a socket file to exist %s", socketFile)
	}
	if stat.Mode()&os.ModeSocket == 0 {
		t.Errorf("Expected the file %s to be a socket file. Got %s", socketFile, stat.Mode().String())
	}
	w := b.workloads.get(UID)
	if w.listener == nil {
		t.Errorf("Expected listener to be set in %+v", w)
	}
	defer w.listener.Close()

	c, err := net.Dial(w.listener.Addr().Network(), w.listener.Addr().String())
	if err != nil {
		t.Errorf("Expected to be able to connected to worker. %+v (%s)", w, err.Error())
	}
	defer c.Close()
}

func TestAddListenerSockFileExists(t *testing.T) {
	dir, err := ioutil.TempDir("", "test-binder")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	UID := "test"
	// Create the mount directory
	mountDir := filepath.Join(dir, MountSubdir, UID)
	if e := os.MkdirAll(mountDir, 0777); e != nil {
		panic(err)
	}

	// create an empty socket file
	socketFile := filepath.Join(mountDir, SocketFilename)
	f, err := os.OpenFile(socketFile, os.O_RDONLY|os.O_CREATE, 0666)
	if err != nil {
		panic(err)
	}
	f.Close()

	testWriteDefaultCredentials(dir, UID)

	ws := newWorkloadStore()
	b := &binder{
		searchPath: dir,
		server:     grpc.NewServer(grpc.Creds(ws)),
		workloads:  ws}

	gotErr := b.addListener(UID)
	if gotErr != nil {
		t.Errorf("Expected no error got %s", gotErr.Error())
	}
	// check if the socketfile is there and is the write mode
	stat, err := os.Stat(socketFile)
	if err != nil {
		t.Errorf("Expected a socket file to exist %s", socketFile)
	}
	if stat.Mode()&os.ModeSocket == 0 {
		t.Errorf("Expected the file %s to be a socket file", socketFile)
	}
	w := b.workloads.get(UID)
	if w.listener == nil {
		t.Errorf("Expected listener to be set in %+v", w)
	}
	defer w.listener.Close()
}

func TestRemoveListener(t *testing.T) {
	dir, err := ioutil.TempDir("", "test-binder")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	UID := "test"
	ls, err := net.Listen("unix", filepath.Join(dir, UID))
	if err != nil {
		panic(err)
	}

	ws := newWorkloadStore()
	w := workload{uid: UID,
		listener: ls}
	ws.store(UID, w)

	b := &binder{
		searchPath: dir,
		server:     grpc.NewServer(grpc.Creds(ws)),
		workloads:  ws}
	b.removeListener(UID)
	//Check that the store is empty for UID
	wGot := ws.get(UID)
	if wGot.uid != "" {
		t.Errorf("Expected empty worload got %+v", wGot)
	}
	lsErr := ls.Close()
	if lsErr == nil {
		t.Errorf("Expected to get error on closing an already closed listener")
	}
}

func TestHandleEvent(t *testing.T) {
	dir, err := ioutil.TempDir("", "test-binder")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	UID := "test"
	// Create the mount directory
	mountDir := filepath.Join(dir, MountSubdir, UID)
	if e := os.MkdirAll(mountDir, 0777); e != nil {
		panic(err)
	}

	testWriteDefaultCredentials(dir, UID)

	ws := newWorkloadStore()
	b := &binder{
		searchPath: dir,
		server:     grpc.NewServer(grpc.Creds(ws)),
		workloads:  ws}

	var workloadEvents = []workloadEvent{{op: Added, uid: UID},
		{op: Removed, uid: UID}}
	for _, e := range workloadEvents {
		b.handleEvent(e)
		switch e.op {
		case Added:
			wGot := ws.get(e.uid)
			if wGot.uid != e.uid {
				t.Errorf("Expected workload store to have UID %s", e.uid)
			}
		case Removed:
			wGot := ws.get(e.uid)
			if wGot.uid != "" {
				t.Errorf("Expected workload store to not have UID %s", e.uid)
			}
		default:
		}
	}
}

func TestSearchAndBind(t *testing.T) {
	dir, err := ioutil.TempDir("", "test-binder")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	UID := "test"
	// Create the mount directory
	mountDir := filepath.Join(dir, MountSubdir, UID)
	if e := os.MkdirAll(mountDir, 0777); e != nil {
		panic(err)
	}

	testWriteDefaultCredentials(dir, UID)

	genListener := func(uid string) net.Listener {
		ln, e := getListener(dir, uid)
		if e != nil {
			panic(e)
		}
		return ln
	}

	ws := newWorkloadStore()
	var workloads = []workload{{"test1", genListener("test1"), Credentials{}},
		{"test2", genListener("test2"), Credentials{}},
	}

	for _, wl := range workloads {
		ws.store(wl.uid, wl)
	}
	b := &binder{
		searchPath: dir,
		server:     grpc.NewServer(grpc.Creds(ws)),
		workloads:  ws}

	stopChan := make(chan interface{})
	go func(c chan interface{}) { c <- nil }(stopChan)

	b.SearchAndBind(stopChan)
	for _, wl := range workloads {
		err := wl.listener.Close()
		if err == nil {
			t.Errorf("Expected to get an error on a closed listener %+v", wl)
		}
	}
}
