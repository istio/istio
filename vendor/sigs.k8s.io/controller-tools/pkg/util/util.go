/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package util

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
)

// FileWriter is a io wrapper to write files
type FileWriter struct {
	Fs afero.Fs
}

// WriteCloser returns a WriteCloser to write to given path
func (fw *FileWriter) WriteCloser(path string) (io.Writer, error) {
	if fw.Fs == nil {
		fw.Fs = afero.NewOsFs()
	}
	dir := filepath.Dir(path)
	err := fw.Fs.MkdirAll(dir, 0700)
	if err != nil {
		return nil, err
	}

	fi, err := fw.Fs.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return nil, err
	}

	return fi, nil
}

// WriteFile write given content to the file path
func (fw *FileWriter) WriteFile(filePath string, content []byte) error {
	if fw.Fs == nil {
		fw.Fs = afero.NewOsFs()
	}
	f, err := fw.WriteCloser(filePath)
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
