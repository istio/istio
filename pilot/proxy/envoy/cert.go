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

package envoy

import (
	"context"
	"hash"
	"io/ioutil"
	"os"
	"path"

	"github.com/golang/glog"
	"github.com/howeyc/fsnotify"
)

// watchCerts watches a certificate directory and calls the provided
// `updateFunc` method when changes are detected. This method is blocking
// so should be run as a goroutine.
func watchCerts(ctx context.Context, certsDir string, updateFunc func()) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		glog.Warning("failed to create a watcher for certificate files")
		return
	}
	defer func() {
		if err := fw.Close(); err != nil {
			glog.Warningf("closing watcher encounters an error %v", err)
		}
	}()

	if err := fw.Watch(certsDir); err != nil {
		glog.Warningf("watching %s encounters an error %v", certsDir, err)
		return
	}

	for {
		select {
		case <-fw.Event:
			glog.V(2).Infof("Change to %q is detected, reload the proxy if necessary", certsDir)
			updateFunc()

		case <-ctx.Done():
			glog.V(2).Info("Certificate watcher is terminated")
			return
		}
	}
}

func generateCertHash(h hash.Hash, certsDir string, files []string) {
	if _, err := os.Stat(certsDir); os.IsNotExist(err) {
		return
	}

	for _, file := range files {
		filename := path.Join(certsDir, file)
		bs, err := ioutil.ReadFile(filename)
		if err != nil {
			glog.Warningf("failed to read file %q", filename)
			continue
		}
		if _, err := h.Write(bs); err != nil {
			glog.Warning(err)
		}
	}
}
