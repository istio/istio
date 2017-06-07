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
package store

import (
	"errors"
	"io/ioutil"
	"os"
	"strings"
	"testing"
)

func TestFSStore(t *testing.T) {
	RunStoreTest(t, func() *TestManager {
		fsroot, _ := ioutil.TempDir("/tmp/", "fsStore")
		f, _ := newFSStore(fsroot)
		_ = os.MkdirAll(fsroot, os.ModeDir|os.ModePerm)
		return NewTestManager(f, func() {
			_ = os.RemoveAll(fsroot)
		})
	})
}

func TestFSStore_FailToCreate(t *testing.T) {
	_, err := newFSStore("/no/such/path")
	if err == nil {
		t.Errorf("expected failure, but succeeded")
	}

	tmpfile, err := ioutil.TempFile("", "dummy")
	if err != nil {
		t.Fatalf("can't create a tempfile for the test: %v", err)
	}
	defer func() { _ = os.Remove(tmpfile.Name()) }()

	_, err = newFSStore(tmpfile.Name())
	if err == nil {
		t.Errorf("expected failure, but succeeded")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("unexpected error %v", err)
	}
}

func TestFSStore_Get(t *testing.T) {
	fsroot, _ := ioutil.TempDir(os.TempDir(), "fsStore")
	store, _ := newFSStore(fsroot)
	f := store.(*fsStore)
	_ = os.MkdirAll(fsroot, os.ModeDir|os.ModePerm)
	defer func(f string) { _ = os.RemoveAll(f) }(fsroot)

	if !strings.Contains(f.String(), fsroot) {
		t.Errorf("Expected %s to contain %s", f, fsroot)
	}

	for _, errs := range []error{os.ErrNotExist,
		errors.New("unexpected error, want logs")} {
		t.Run(errs.Error(), func(t *testing.T) {
			f.readfile = func(filename string) ([]byte, error) {
				return []byte{}, errs
			}
			// when file does not exist
			_, _, found := f.Get("k1")
			if found {
				t.Error("unexpectedly found file")
			}
		})
	}
}

func TestFSStore_SetErrors(t *testing.T) {
	fsroot, _ := ioutil.TempDir(os.TempDir(), "fsStore")
	_ = os.MkdirAll(fsroot, os.ModeDir|os.ModePerm)
	defer func(f string) { _ = os.RemoveAll(f) }(fsroot)

	for _, tt := range []struct {
		when string
		err  error
	}{
		{"", errors.New("file creation error")},
		{"write", errors.New("write error")},
		{"mkdir", errors.New("mkdir error")},
		{"close", errors.New("close error")},
	} {
		t.Run(tt.when, func(t *testing.T) {
			store, _ := newFSStore(fsroot)
			f := store.(*fsStore)
			if tt.when == "mkdir" {
				f.mkdirAll = func(path string, perm os.FileMode) error {
					return tt.err
				}
			} else {
				f.tempFile = func() (ff writeCloser, err error) {
					if tt.when == "" {
						return nil, tt.err
					}
					return &fakeWriteCloser{err: tt.err, when: tt.when}, nil
				}
			}
			_, err1 := f.Set("k1", "v1")
			if err1 != tt.err {
				t.Errorf("got %s\nwant %s", err1, tt.err)
			}
		})
	}
}

type fakeWriteCloser struct {
	err  error
	when string
}

func (f *fakeWriteCloser) Write(p []byte) (n int, err error) {
	if f.when == "write" {
		return -1, f.err
	}
	return len(p), nil
}

func (f *fakeWriteCloser) Close() error {
	if f.when == "close" {
		return f.err
	}
	return nil
}

func (f *fakeWriteCloser) Name() string { return "fakeWriteCloser" }

func TestFSStore_Delete(t *testing.T) {
	fsroot, _ := ioutil.TempDir(os.TempDir(), "fsStore")
	store, _ := newFSStore(fsroot)
	f := store.(*fsStore)
	_ = os.MkdirAll(fsroot, os.ModeDir|os.ModePerm)
	defer func(f string) { _ = os.RemoveAll(f) }(fsroot)

	for _, tst := range []struct {
		err     error
		success bool
	}{{os.ErrNotExist, true},
		{errors.New("unexpected error, want logs"), false},
	} {
		t.Run(tst.err.Error(), func(t *testing.T) {
			f.remove = func(name string) error {
				return tst.err
			}
			err := f.Delete("K1")
			if tst.success && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

		})
	}
}
