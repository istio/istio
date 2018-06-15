//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package internal

import (
	"io/ioutil"
	"os"
	"path"
	"path/filepath"

	"istio.io/istio/pkg/log"
)

// TempFile represents a temporary file that was created.
type TempFile string

// NewTempFile creates and returns the name of a new temp file.
func NewTempFile(tmpDir, prefix, suffix string) (TempFile, error) {
	f, err := ioutil.TempFile(tmpDir, prefix)
	if err != nil {
		return "", err
	}
	var tmpName string
	if tmpName, err = filepath.Abs(f.Name()); err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	if err = os.Remove(tmpName); err != nil {
		log.Errorf("CreateTempfile unable to remove %s", tmpName)
		return "", err
	}
	return TempFile(tmpName + suffix), nil
}

// WriteTempFile creates a temporary file with the specified contents.
func WriteTempFile(tmpDir, prefix, suffix, contents string) (TempFile, error) {
	fname, err := NewTempFile(tmpDir, prefix, suffix)
	if err != nil {
		return "", err
	}

	if err = ioutil.WriteFile(string(fname), []byte(contents), 0644); err != nil {
		return "", err
	}
	return fname, nil
}

// Delete the temporary file.
func (t TempFile) Delete() {
	err := os.Remove(string(t))
	if err != nil {
		log.Errorf("Unable to remove %s: %v", t, err)
	}
}

// createTmpDirectory creates a temporary directory for running local programs, or storing logs.
// By default, the root of the tmp dir will be established by os.TempDir(). If workdir flag is specified,
// it will be used instead.
// The directory will be of the form <root>/<runID>/<name>/.
func createTmpDirectory(workdir string, runID string, name string) (string, error) {

	dir := workdir
	if dir == "" {
		dir = os.TempDir()
	}

	dir = path.Join(dir, runID, name)
	if err := os.MkdirAll(dir, 0777); err != nil {
		return "", err
	}

	scope.Debugf("Created a temp dir: runID='%s', name='%s', location='%s'", runID, name, dir)

	return dir, nil
}
