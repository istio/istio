// Copyright Istio Authors
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

package file

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"istio.io/istio/pkg/test/util/assert"
)

func copyTest(t *testing.T, copyFn func(srcFilepath, targetDir, targetFilename string) error) {
	t.Helper()
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "in"), []byte("hello world"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := copyFn(filepath.Join(d, "in"), d, "out"); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(d, "out"))
	if err != nil {
		t.Fatal(err)
	}
	m, _ := f.Stat()
	// Mode should be copied
	assert.Equal(t, m.Mode(), 0o750)

	body, _ := io.ReadAll(f)
	// Contents should be copied
	assert.Equal(t, body, []byte("hello world"))
}

func TestCopy(t *testing.T) {
	copyTest(t, Copy)
}

func TestAtomicCopy(t *testing.T) {
	copyTest(t, AtomicCopy)
}

func TestAtomicWrite(t *testing.T) {
	d := t.TempDir()
	file := filepath.Join(d, "test")
	data := []byte("hello world")
	err := AtomicWrite(file, data, 0o750)
	assert.NoError(t, err)
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, body, data)
}

func TestExists(t *testing.T) {
	d := t.TempDir()
	exist := Exists(d)
	assert.Equal(t, exist, true)

	unExist := Exists("unExist")
	assert.Equal(t, unExist, false)
}

func TestIsDirWriteable(t *testing.T) {
	d := t.TempDir()
	err := IsDirWriteable(d)
	assert.NoError(t, err)

	err = IsDirWriteable("unWriteAble")
	assert.Error(t, err)
}
