// Copyright 2018 The Operator-SDK Authors
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

// Modified from github.com/kubernetes-sigs/controller-tools/pkg/util/util.go

package fileutil

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"
)

const (
	// file modes
	DefaultDirFileMode  = 0750
	DefaultFileMode     = 0644
	DefaultExecFileMode = 0755

	DefaultFileFlags = os.O_WRONLY | os.O_CREATE
)

// FileWriter is a io wrapper to write files
type FileWriter struct {
	fs   afero.Fs
	once sync.Once
}

func NewFileWriterFS(fs afero.Fs) *FileWriter {
	fw := &FileWriter{}
	fw.once.Do(func() {
		fw.fs = fs
	})
	return fw
}

func (fw *FileWriter) GetFS() afero.Fs {
	fw.once.Do(func() {
		fw.fs = afero.NewOsFs()
	})
	return fw.fs
}

// WriteCloser returns a WriteCloser to write to given path
func (fw *FileWriter) WriteCloser(path string, mode os.FileMode) (io.Writer, error) {
	dir := filepath.Dir(path)
	err := fw.GetFS().MkdirAll(dir, DefaultDirFileMode)
	if err != nil {
		return nil, err
	}

	fi, err := fw.GetFS().OpenFile(path, DefaultFileFlags, mode)
	if err != nil {
		return nil, err
	}

	return fi, nil
}

// WriteFile write given content to the file path
func (fw *FileWriter) WriteFile(filePath string, content []byte) error {
	f, err := fw.WriteCloser(filePath, DefaultFileMode)
	if err != nil {
		return fmt.Errorf("failed to create %s: %v", filePath, err)
	}

	if c, ok := f.(io.Closer); ok {
		defer func() {
			if err := c.Close(); err != nil {
				log.Fatal(err)
			}
		}()
	}

	_, err = f.Write(content)
	if err != nil {
		return fmt.Errorf("failed to write %s: %v", filePath, err)
	}

	return nil
}

func IsClosedError(e error) bool {
	pathErr, ok := e.(*os.PathError)
	if !ok {
		return false
	}
	if pathErr.Err == os.ErrClosed {
		return true
	}
	return false
}
