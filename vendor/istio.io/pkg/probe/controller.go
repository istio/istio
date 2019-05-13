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
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	multierror "github.com/hashicorp/go-multierror"

	"istio.io/pkg/log"
)

// Controller provides the internal interface to handle events coming
// from Emitters. Individual controller implementation will update
// its prober status.
type Controller interface {
	io.Closer
	Start()
	register(p *Probe, initial error)
	onChange(p *Probe, newStatus error)
}

type controllerImpl interface {
	onClose() error
	onUpdate(newStatus error)
}

type controller struct {
	sync.Mutex
	statuses map[*Probe]error

	name     string
	closec   chan struct{}
	donec    chan struct{}
	interval time.Duration
	impl     controllerImpl
}

func (cb *controller) Start() {
	if cb.closec != nil {
		close(cb.closec)
	}

	cb.closec = make(chan struct{})
	cb.donec = make(chan struct{})
	cb.impl.onUpdate(cb.status())
	go func() {
		// Considering the consumption of the updating status, invoke
		// onUpdate every half of the specified interval.
		t := time.NewTicker(cb.interval / 2)
	loop:
		for {
			select {
			case <-cb.closec:
				break loop
			case <-t.C:
				cb.impl.onUpdate(cb.status())
			}
		}
		close(cb.donec)
	}()
}

func (cb *controller) register(p *Probe, initial error) {
	cb.Lock()
	defer cb.Unlock()
	if _, ok := cb.statuses[p]; ok {
		log.Debugf("%s is already registered to %s", p, cb.name)
		return
	}
	cb.statuses[p] = initial
}

func (cb *controller) status() error {
	cb.Lock()
	defer cb.Unlock()
	return cb.statusLocked()
}

func (cb *controller) statusLocked() (err error) {
	if len(cb.statuses) == 0 {
		return fmt.Errorf("%s has no emitters", cb.name)
	}
	for p, status := range cb.statuses {
		if status != nil {
			err = multierror.Append(err, fmt.Errorf("%s: %v", p, status))
		}
	}
	return err
}

func (cb *controller) onChange(p *Probe, newStatus error) {
	cb.Lock()
	defer cb.Unlock()
	prev := cb.statusLocked()
	if stat, ok := cb.statuses[p]; !ok || stat == newStatus {
		return
	}
	cb.statuses[p] = newStatus
	curr := cb.statusLocked()
	if prev != curr && (prev == nil || curr == nil) {
		if prev == nil && curr != nil {
			log.Errorf("%s turns unavailable: %v", cb.name, curr)
		} else if prev != nil {
			log.Debugf("%s error status: %v", cb.name, curr)
		}
	}
}

func (cb *controller) Close() error {
	cb.Lock()
	cb.statuses = map[*Probe]error{}
	close(cb.closec)
	cb.Unlock()
	<-cb.donec
	return cb.impl.onClose()
}

type fileController struct {
	path string
}

// NewFileController creates a new Controller implementation which creates a file
// at the specified path only when the registered emitters are all available.
func NewFileController(opt *Options) Controller {
	name := filepath.Base(opt.Path)
	return &controller{
		statuses: map[*Probe]error{},
		name:     name,
		interval: opt.UpdateInterval,
		impl:     &fileController{path: opt.Path},
	}
}

func (fc *fileController) onClose() error {
	if _, err := os.Stat(fc.path); os.IsNotExist(err) {
		return nil
	}
	return os.Remove(fc.path)
}

func (fc *fileController) onUpdate(newStatus error) {
	if newStatus == nil {
		// os.Create updates the last modified time even when the path exists, so
		// simply calling this creates or updates the modified time.
		f, err := os.Create(fc.path)
		if err != nil {
			log.Errorf("Failed to create the path %s: %v", fc.path, err)
			return
		}
		_ = f.Close()
	} else if err := os.Remove(fc.path); err != nil && !os.IsNotExist(err) {
		log.Errorf("Failed to remove the path %s: %v", fc.path, err)
	}
}
