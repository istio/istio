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
package config

import (
	"errors"
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"strings"
	"testing"
)

func getContents(key string) string {
	return key
}

func TestFSStore(t *testing.T) {
	testStore(t, func() *KVMgr {
		fsroot, _ := ioutil.TempDir("/tmp/", "fsStore")
		f := newFSStore(fsroot)
		_ = os.MkdirAll(fsroot, os.ModeDir|os.ModePerm)
		return &KVMgr{f, func() {
			_ = os.RemoveAll(fsroot)
		}}
	})
}

type KVMgr struct {
	store   KeyValueStore
	cleanup func()
}

func (k *KVMgr) Get() KeyValueStore { return k.store }
func (k *KVMgr) Cleanup()           { k.cleanup() }

func testStore(t *testing.T, kvMgrfn func() *KVMgr) {
	GOODKEYS := []string{
		"/scopes/global/adapters",
		"/scopes/global/descriptors",
		"/scopes/global/subjects/global/rules",
		"/scopes/global/subjects/svc1.ns.cluster.local/rules",
	}

	table := []struct {
		desc       string
		keys       []string
		listPrefix string
		listKeys   []string
	}{
		{"goodkeys", GOODKEYS, "/scopes/global/subjects",
			[]string{"/scopes/global/subjects/global/rules",
				"/scopes/global/subjects/svc1.ns.cluster.local/rules"},
		},
		{"goodkeys", GOODKEYS, "/scopes/", GOODKEYS},
	}

	for _, tt := range table {
		km := kvMgrfn()
		s := km.Get()
		t.Run(tt.desc, func(t1 *testing.T) {
			var found bool
			badkey := "a/b"
			_, _, found = s.Get(badkey)
			if found {
				t.Errorf("Unexpectedly found %s", badkey)
			}

			var val string
			// create keys
			for _, key := range tt.keys {
				kc := getContents(key)
				_, err := s.Set(key, kc)
				if err != nil {
					t.Errorf("Unexpected error for %s: %v", key, err)
				}
				val, _, found = s.Get(key)
				if !found || kc != val {
					t.Errorf("got %s\nwant %s", val, kc)
				}
			}
			k, _, err := s.List(tt.listPrefix, true)
			if err != nil {
				t.Error("Unexpected error", err)
			}
			if !reflect.DeepEqual(k, tt.listKeys) {
				t.Errorf("Got %s\nWant %s\n", k, tt.listKeys)
			}
			err = s.Delete(k[1])
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			_, _, found = s.Get(k[1])
			if found {
				t.Errorf("Unexpectedly found %s", k[1])
			}

		})
		km.Cleanup()
	}
}

func TestNewStore(t *testing.T) {
	for _, tt := range []struct {
		url string
		err error
	}{
		{"fs:///tmp/testdata/configroot", nil},
		{"redis://:password@hostname:port/db_number", errors.New("not implemented")},
		{"etcd:///tmp/testdata/configroot", errors.New("unknown")},
		{"/tmp/testdata/configroot", errors.New("invalid")},
	} {
		t.Run(tt.url, func(t *testing.T) {
			_, err := NewStore(tt.url)
			if err == tt.err {
				return
			}

			if err != nil {
				if tt.err == nil || !strings.Contains(err.Error(), tt.err.Error()) {
					t.Errorf("got %s\nwant %s", err, tt.err)
				}
			}
		})
	}
}

func TestFSStore_Get(t *testing.T) {
	fsroot, _ := ioutil.TempDir(os.TempDir(), "fsStore")
	f := newFSStore(fsroot).(*fsStore)
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
			f := newFSStore(fsroot).(*fsStore)
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
	f := newFSStore(fsroot).(*fsStore)
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

func writeFile(t *testing.T, filename, contents string) {
	if err := ioutil.WriteFile(filename, []byte(contents), os.ModePerm); err != nil {
		t.Fatalf("unable to create file %s", filename)
	}
}

func assertKey(t *testing.T, store KeyValueStore, key, want string) {
	if s, _, _ := store.Get(key); s != want {
		t.Fatalf("got %s want %s", s, want)
	}
}

func TestNewCompatFSStore(t *testing.T) {
	dir, _ := ioutil.TempDir("", "FTEST")
	defer func(name string) { _ = os.RemoveAll(name) }(dir)

	gc := "Global"
	sc := "Service"
	writeFile(t, path.Join(dir, gc), gc)
	writeFile(t, path.Join(dir, sc), sc)

	store, err := NewCompatFSStore(path.Join(dir, gc), path.Join(dir, sc))
	if err != nil {
		t.Fatalf("unexpected error %s", err.Error())
	}

	assertKey(t, store, keyGlobalServiceConfig, sc)
	assertKey(t, store, keyDescriptors, gc)
}
