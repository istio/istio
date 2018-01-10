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

package probe

import (
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestFileController(t *testing.T) {
	dir, err := ioutil.TempDir("", t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "foo")
	fc := NewFileController(path)
	if err = PathExists(path); err == nil {
		t.Errorf("Path %s shouldn't exist.", path)
	}

	p1 := NewProbe()
	p1.RegisterProbe(fc, "p1")

	p1.SetAvailable(nil)
	if err = PathExists(path); err != nil {
		t.Errorf("Path %s should exist: %v", path, err)
	}

	p2 := NewProbe()
	p2.RegisterProbe(fc, "p2")

	if err = PathExists(path); err == nil {
		t.Errorf("Path %s shouldn't exist.", path)
	}

	p2.SetAvailable(nil)
	if err = PathExists(path); err != nil {
		t.Errorf("Path %s should exist: %v", path, err)
	}

	p1.SetAvailable(errors.New("dummy"))
	if err = PathExists(path); err == nil {
		t.Errorf("Path %s shouldn't exist.", path)
	}
}

func TestControllerRegisterTwice(t *testing.T) {
	dir, err := ioutil.TempDir("", t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "foo")
	fc := NewFileController(path)

	p1 := NewProbe()
	p1.RegisterProbe(fc, "p1")
	p1.RegisterProbe(fc, "p1")

	if err = PathExists(path); err == nil {
		t.Errorf("Path %s shouldn't exist.", path)
	}

	p1.SetAvailable(nil)
	if err = PathExists(path); err != nil {
		t.Errorf("Path %s should exist: %v", path, err)
	}
}

func TestControllerAfterClose(t *testing.T) {
	dir, err := ioutil.TempDir("", t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "foo")
	fc := NewFileController(path)

	p1 := NewProbe()
	p1.RegisterProbe(fc, "p1")

	if err = PathExists(path); err == nil {
		t.Errorf("Path %s shouldn't exist.", path)
	}

	p1.SetAvailable(nil)
	if err = PathExists(path); err != nil {
		t.Errorf("Path %s should exist: %v", path, err)
	}

	if err = fc.Close(); err != nil {
		t.Errorf("failed to close: %v", err)
	}
	if err = PathExists(path); err == nil {
		t.Errorf("Path %s shouldn't exist.", path)
	}

	p1.SetAvailable(errors.New("dummy"))
	if err = PathExists(path); err == nil {
		t.Errorf("Path %s shouldn't exist.", path)
	}
	p1.SetAvailable(nil)
	if err = PathExists(path); err == nil {
		t.Errorf("Path %s shouldn't exist.", path)
	}
}
