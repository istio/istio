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

//go:generate go-bindata --nocompress --nometadata --pkg vfs -o assets.gen.go --prefix ../../data ../../data/...

// Package vfs is a set of file system utilities to access compiled-in helm charts.
package vfs

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ReadFile reads the content of compiled in files at path and returns a buffer with the data.
func ReadFile(path string) ([]byte, error) {
	return Asset(path)
}

// Stat returns a FileInfo object for the given path.
func Stat(path string) (os.FileInfo, error) {
	info, err := AssetInfo(path)
	if err != nil {
		// try it as a directory instead
		_, err = AssetDir(path)
		if err == nil {
			info = &dirInfo{name: filepath.Base(path)}
		}
	} else {
		fi := info.(bindataFileInfo)
		fi.name = filepath.Base(fi.name)
	}

	return info, err
}

type dirInfo struct {
	name string
}

func (di dirInfo) Name() string {
	return di.name
}
func (di dirInfo) Size() int64 {
	return 0
}
func (di dirInfo) Mode() os.FileMode {
	return os.FileMode(0)
}
func (di dirInfo) ModTime() time.Time {
	return time.Unix(0, 0)
}
func (di dirInfo) IsDir() bool {
	return true
}
func (di dirInfo) Sys() interface{} {
	return nil
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
	return AssetDir(path)
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
	fs, _ := AssetDir(prefix)
	var out []string
	for _, f := range fs {
		info, err := Stat(filepath.Join(prefix, f))
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			out = append(out, filepath.Join(prefix, filepath.Base(info.Name())))
			continue
		}
		nfs, err := getFilesRecursive(prefix, info)
		if err != nil {
			return nil, err
		}
		out = append(out, nfs...)
	}
	return out, nil
}
