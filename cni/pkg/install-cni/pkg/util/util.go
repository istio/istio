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

package util

import (
	"context"
	"github.com/coreos/etcd/pkg/fileutil"
	"github.com/fsnotify/fsnotify"
	"github.com/pkg/errors"
	"io/ioutil"
	"os"
	"path/filepath"
)

// Write atomically by writing to a temporary file in the same directory then renaming
func WriteAtomically(path string, data []byte, mode os.FileMode) (err error) {
	tmpFile, err := ioutil.TempFile(filepath.Dir(path), filepath.Base(path)+".tmp.")
	if err != nil {
		return
	}
	defer func() {
		if fileutil.Exist(tmpFile.Name()) {
			if rmErr := os.Remove(tmpFile.Name()); rmErr != nil {
				if err != nil {
					err = errors.Wrap(err, rmErr.Error())
				} else {
					err = rmErr
				}
			}
		}
	}()

	if err = os.Chmod(tmpFile.Name(), mode); err != nil {
		return
	}

	_, err = tmpFile.Write(data)
	if err != nil {
		if closeErr := tmpFile.Close(); closeErr != nil {
			err = errors.Wrap(err, closeErr.Error())
		}
		return
	}
	if err = tmpFile.Close(); err != nil {
		return
	}

	err = os.Rename(tmpFile.Name(), path)
	return
}

func CreateFileWatcher(dir string) (watcher *fsnotify.Watcher, fileModified chan bool, errChan chan error, err error) {
	watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return
	}

	fileModified, errChan = make(chan bool), make(chan error)
	go WatchFiles(watcher, fileModified, errChan)

	if err = watcher.Add(dir); err != nil {
		if closeErr := watcher.Close(); closeErr != nil {
			err = errors.Wrap(err, closeErr.Error())
		}
		return nil, nil, nil, err
	}

	return
}

func WatchFiles(watcher *fsnotify.Watcher, fileModified chan bool, errChan chan error) {
	for {
		select {
		case _, ok := <-watcher.Events:
			if !ok {
				return
			}
			fileModified <- true
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			errChan <- err
		}
	}
}

func WaitForFileMod(ctx context.Context, fileModified chan bool, errChan chan error) error {
	select {
	case <-fileModified:
		return nil
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
