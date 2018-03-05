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

package binder

import (
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	fv "istio.io/istio/security/pkg/flexvolume"
)

type watcher interface {
	// Returns a channel that sends events whenever workload mounts are created or removed
	watch(stop <-chan bool) <-chan workloadEvent
}

type operation int

const (
	added operation = iota
	removed
)

// credentialsSubdir is where the credential files are created by flex volume driver
const credentialsSubdir = "creds"
const pollSleepTime = 100 * time.Millisecond

type workloadEvent struct {
	// What happened to the workload mount?
	op  operation
	uid string
}

// newWatcher watches path's CredentialSubdir for workload creation/deletion
func newWatcher(path string) watcher {
	return &pollWatcher{path}
}

type pollWatcher struct {
	path string
}

func (p *pollWatcher) watch(stop <-chan bool) <-chan workloadEvent {
	c := make(chan workloadEvent)
	go p.poll(c, stop)
	return c
}

func (p *pollWatcher) poll(events chan<- workloadEvent, stop <-chan bool) {
	// The workloads we know about.
	known := make(map[string]bool)
	credPath := filepath.Join(p.path, credentialsSubdir)
	for {
		// check if we need to stop polling.
		select {
		case <-stop:
			close(events)
			return
		default:
			//continue
		}
		if _, err := os.Stat(credPath); err != nil {
			time.Sleep(pollSleepTime)
		} else {
			break
		}
	}
	log.Println("Ready to parse credential directory")
	for {
		// check if we need to stop polling.
		select {
		case <-stop:
			close(events)
			return
		default:
			//continue
		}

		// list the contents of the directory
		files, err := ioutil.ReadDir(credPath)
		if err != nil {
			log.Printf("error reading %s: %v", p.path, err)
			close(events)
			return
		}
		// This set will contain all previously known UIDs that are now absent.
		removedWls := copyStringSet(known)
		for _, file := range files {
			isCred, uid := parseFilename(file.Name())
			if isCred {
				if !known[uid] {
					events <- workloadEvent{op: added, uid: uid}
					known[uid] = true
				}
				delete(removedWls, uid)
			}
		}
		// Send updates for UIDs we no longer know about.
		for uid := range removedWls {
			events <- workloadEvent{op: removed, uid: uid}
			delete(known, uid)
		}
		time.Sleep(pollSleepTime)
	}
}

func parseFilename(name string) (isCred bool, uid string) {
	if strings.HasSuffix(name, fv.CredentialFileExtension) {
		return true, strings.TrimSuffix(name, fv.CredentialFileExtension)
	}
	return false, ""
}

func copyStringSet(original map[string]bool) map[string]bool {
	n := make(map[string]bool)
	for k, v := range original {
		n[k] = v
	}
	return n
}
