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
	"crypto/sha256"
	"hash"
	"io/ioutil"
	"os"
	"path"
	"time"

	"github.com/howeyc/fsnotify"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

const (
	// defaultMinDelay is the minimum amount of time between delivery of two successive events via updateFunc.
	defaultMinDelay = 10 * time.Second
)

// Watcher triggers reloads on changes to the proxy config
type Watcher interface {
	// Run the watcher loop (blocking call)
	Run(context.Context)
}

// CertSource is file source for certificates
type CertSource struct {
	// Directory containing certificates
	Directory string
	// Files for certificates
	Files []string
}

type watcher struct {
	role     model.Proxy
	config   meshconfig.ProxyConfig
	certs    []CertSource
	pilotSAN []string
	updates  chan<- interface{}
}

// NewWatcher creates a new watcher instance from a proxy agent and a set of monitored certificate paths
// (directories with files in them)
func NewWatcher(
	config meshconfig.ProxyConfig,
	role model.Proxy,
	certs []CertSource,
	pilotSAN []string,
	updates chan<- interface{}) Watcher {
	return &watcher{
		role:     role,
		config:   config,
		certs:    certs,
		pilotSAN: pilotSAN,
		updates:  updates,
	}
}

func (w *watcher) Run(ctx context.Context) {
	// kick start the proxy with partial state (in case there are no notifications coming)
	w.SendConfig()

	// monitor certificates
	certDirs := make([]string, 0, len(w.certs))
	for _, cert := range w.certs {
		certDirs = append(certDirs, cert.Directory)
	}

	go watchCerts(ctx, certDirs, watchFileEvents, defaultMinDelay, w.SendConfig)

	<-ctx.Done()
}

func (w *watcher) SendConfig() {
	h := sha256.New()
	for _, cert := range w.certs {
		generateCertHash(h, cert.Directory, cert.Files)
	}

	w.updates <- h.Sum(nil)
}

type watchFileEventsFn func(ctx context.Context, wch <-chan *fsnotify.FileEvent,
	minDelay time.Duration, notifyFn func())

// watchFileEvents watches for changes on a channel and notifies via notifyFn().
// The function batches changes so that related changes are processed together.
// The function ensures that notifyFn() is called no more than one time per minDelay.
// The function does not return until the the context is cancelled.
func watchFileEvents(ctx context.Context, wch <-chan *fsnotify.FileEvent, minDelay time.Duration, notifyFn func()) {
	// timer and channel for managing minDelay.
	var timeChan <-chan time.Time
	var timer *time.Timer

	for {
		select {
		case ev := <-wch:
			log.Infof("watchFileEvents: %s", ev.String())
			if timer != nil {
				continue
			}
			// create new timer
			timer = time.NewTimer(minDelay)
			timeChan = timer.C
		case <-timeChan:
			// reset timer
			timeChan = nil
			timer.Stop()
			timer = nil

			log.Info("watchFileEvents: notifying")
			notifyFn()
		case <-ctx.Done():
			log.Info("watchFileEvents has terminated")
			return
		}
	}
}

// watchCerts watches all certificate directories and calls the provided
// `updateFunc` method when changes are detected. This method is blocking
// so it should be run as a goroutine.
// updateFunc will not be called more than one time per minDelay.
func watchCerts(ctx context.Context, certsDirs []string, watchFileEventsFn watchFileEventsFn,
	minDelay time.Duration, updateFunc func()) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		log.Warnf("failed to create a watcher for certificate files: %v", err)
		return
	}
	defer func() {
		if err := fw.Close(); err != nil {
			log.Warnf("closing watcher encounters an error %v", err)
		}
	}()

	// watch all directories
	for _, d := range certsDirs {
		if err := fw.Watch(d); err != nil {
			log.Warnf("watching %s encounters an error %v", d, err)
			return
		}
	}
	watchFileEventsFn(ctx, fw.Event, minDelay, updateFunc)
}

func generateCertHash(h hash.Hash, certsDir string, files []string) {
	if _, err := os.Stat(certsDir); os.IsNotExist(err) {
		return
	}

	for _, file := range files {
		filename := path.Join(certsDir, file)
		bs, err := ioutil.ReadFile(filename)
		if err != nil {
			// log.Warnf("failed to read file %q", filename)
			continue
		}
		if _, err := h.Write(bs); err != nil {
			log.Warna(err)
		}
	}
}
