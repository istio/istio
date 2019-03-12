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
	"time"

	"github.com/howeyc/fsnotify"

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

type watcher struct {
	certs   []string
	updates chan<- interface{}
}

// NewWatcher creates a new watcher instance from a proxy agent and a set of monitored certificate file paths
func NewWatcher(
	certs []string,
	updates chan<- interface{}) Watcher {
	return &watcher{
		certs:   certs,
		updates: updates,
	}
}

func (w *watcher) Run(ctx context.Context) {
	// kick start the proxy with partial state (in case there are no notifications coming)
	w.SendConfig()

	// monitor certificates
	go watchCerts(ctx, w.certs, watchFileEvents, defaultMinDelay, w.SendConfig)

	<-ctx.Done()
	log.Info("Watcher has successfully terminated")
}

func (w *watcher) SendConfig() {
	h := sha256.New()
	generateCertHash(h, w.certs)
	w.updates <- h.Sum(nil)
}

type watchFileEventsFn func(ctx context.Context, wch <-chan *fsnotify.FileEvent,
	minDelay time.Duration, notifyFn func())

// watchFileEvents watches for changes on a channel and notifies via notifyFn().
// The function batches changes so that related changes are processed together.
// The function ensures that notifyFn() is called no more than one time per minDelay.
// The function does not return until the the context is canceled.
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
			log.Info("watchFileEvents has successfully terminated")
			return
		}
	}
}

// watchCerts watches all certificates and calls the provided
// `updateFunc` method when changes are detected. This method is blocking
// so it should be run as a goroutine.
// updateFunc will not be called more than one time per minDelay.
func watchCerts(ctx context.Context, certs []string, watchFileEventsFn watchFileEventsFn,
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

	// watch all files
	for _, c := range certs {
		if err := fw.Watch(c); err != nil {
			log.Warnf("watching %s encounters an error %v", c, err)
			return
		}
		log.Infof("watching %s for changes", c)
	}
	watchFileEventsFn(ctx, fw.Event, minDelay, updateFunc)
}

func generateCertHash(h hash.Hash, certs []string) {
	for _, cert := range certs {
		if _, err := os.Stat(cert); os.IsNotExist(err) {
			continue
		}
		bs, err := ioutil.ReadFile(cert)
		if err != nil {
			// log.Warnf("failed to read file %q", filename)
			continue
		}
		if _, err := h.Write(bs); err != nil {
			log.Warna(err)
		}
	}
}
