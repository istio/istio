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
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/golang/glog"
)

// fsStore implements file system KeyValueStore and change store.
type fsStore struct {
	// root directory
	root string
	// tmpdir is used as a scratch pad area
	tmpdir string
	// file suffix
	suffix string

	// testing and fault injection
	tempFile writeCloserFunc
	readfile readFileFunc
	mkdirAll mkdirAllFunc
	remove   removeFunc
}

// for testing
type writeCloser interface {
	io.WriteCloser
	Name() string
}
type writeCloserFunc func() (f writeCloser, err error)
type readFileFunc func(filename string) ([]byte, error)
type mkdirAllFunc func(path string, perm os.FileMode) error
type removeFunc func(name string) error

func newFSStore(root string) (KeyValueStore, error) {
	finfo, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !finfo.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}
	s := &fsStore{
		root:     root,
		tmpdir:   root + "/TMP",
		suffix:   ".yml",
		readfile: ioutil.ReadFile,
		mkdirAll: os.MkdirAll,
		remove:   os.Remove,
	}

	s.tempFile = func() (f writeCloser, err error) {
		if err = s.mkdirAll(s.tmpdir, os.ModeDir|os.ModePerm); err != nil {
			return nil, err
		}
		f, err = ioutil.TempFile(s.tmpdir, "fsStore")
		return f, err
	}
	return s, nil
}

func (f *fsStore) String() string {
	return fmt.Sprintf("fsStore: %s", f.root)
}

func (f *fsStore) Close() {
	// Do nothing.
}

func (f *fsStore) getPath(key string) string {
	return path.Join(f.root, key)
}

// Get value at a key, false if not found.
func (f *fsStore) Get(key string) (value string, index int, found bool) {
	p := f.getPath(key) + f.suffix
	var b []byte
	var err error

	if b, err = f.readfile(p); err != nil {
		if !os.IsNotExist(err) {
			glog.Warningf("Could not access '%s': %v", p, err)
		}
		return "", IndexNotSupported, false
	}
	return string(b), IndexNotSupported, true
}

// Set a value
func (f *fsStore) Set(key string, value string) (index int, err error) {
	p := f.getPath(key) + f.suffix
	if err = f.mkdirAll(filepath.Dir(p), os.ModeDir|os.ModePerm); err != nil {
		return IndexNotSupported, err
	}

	var tf writeCloser
	if tf, err = f.tempFile(); err != nil {
		return IndexNotSupported, err
	}

	_, err = tf.Write([]byte(value))
	// always close the file and return the 1st failure.
	errClose := tf.Close()
	if err != nil {
		return IndexNotSupported, err
	}
	if errClose != nil {
		return IndexNotSupported, errClose
	}
	// file has been written and closed.
	// atomically rename
	return IndexNotSupported, os.Rename(tf.Name(), p)
}

// List keys with the prefix
func (f *fsStore) List(key string, recurse bool) (keys []string, index int, err error) {
	keys = make([]string, 0, 10)
	cc := &keys

	err = filepath.Walk(f.getPath(key), func(path string, info os.FileInfo, err error) error {
		if strings.HasSuffix(path, f.suffix) {
			*cc = append(*cc, path[len(f.root):len(path)-len(f.suffix)])
		}
		return nil
	})
	return keys, IndexNotSupported, err
}

// Delete removes a key from the fs store.
func (f *fsStore) Delete(key string) (err error) {
	p := f.getPath(key) + f.suffix
	if err = f.remove(p); err == nil || os.IsNotExist(err) {
		return nil
	}
	return err
}
