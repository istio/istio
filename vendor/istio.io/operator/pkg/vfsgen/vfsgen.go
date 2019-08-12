// Copyright 2019 Istio Authors
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

/*
Package vfsgen is a set of file system utilities for compiled in helm charts which are generated using
github.com/shurcooL/vfsgen and included in this package in vfsgen.gen.go.
*/
package vfsgen

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ReadFile reads the content of compiled in files at path and returns a buffer with the data.
func ReadFile(path string) ([]byte, error) {
	f, err := Assets.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	b := make([]byte, fi.Size())

	if _, err := f.Read(b); err != nil && err != io.EOF {
		return nil, err
	}
	return b, nil
}

// Stat returns a FileInfo object for the given path.
func Stat(path string) (os.FileInfo, error) {
	f, err := Assets.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Stat()
}

// Size returns the size of the file at the given path, if it is found.
func Size(path string) (int64, error) {
	n, err := Stat(path)
	if err != nil {
		return 0, err
	}
	return n.Size(), nil
}

// ReadDir non-recursively reads the directory at path and returns all the files contained in it.
func ReadDir(path string) ([]string, error) {
	dir, err := Assets.Open(path)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	fs, err := dir.Readdir(-1)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range fs {
		out = append(out, f.Name())
	}
	return out, nil
}

// GetFilesRecursive recursively reads the directory at path and returns all the files contained in it.
func GetFilesRecursive(path string) ([]string, error) {
	rootFI, err := Stat(path)
	if err != nil {
		return nil, err
	}
	return getFilesRecursive(filepath.Dir(path), rootFI)
}

func getFilesRecursive(prefix string, root os.FileInfo) ([]string, error) {
	if !root.IsDir() {
		return nil, fmt.Errorf("not a dir: %s", root.Name())
	}
	prefix = filepath.Join(prefix, root.Name())
	dir, err := Assets.Open(prefix)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	fs, err := dir.Readdir(-1)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range fs {
		if !f.IsDir() {
			out = append(out, filepath.Join(prefix, f.Name()))
			continue
		}
		nfs, err := getFilesRecursive(prefix, f)
		if err != nil {
			return nil, err
		}
		out = append(out, nfs...)
	}
	return out, nil
}
