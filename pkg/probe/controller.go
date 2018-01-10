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
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/hashicorp/go-multierror"
	"istio.io/istio/pkg/log"
)

var errUnregistered = errors.New("unregistered")

// Controller provides the internal interface to handle events coming
// from Emitters. Individual controller implementation will update
// its prober status.
type Controller interface {
	io.Closer
	register(p *Probe, initial error)
	onChange(p *Probe, newStatus error)
}

type controllerBase struct {
	sync.Mutex
	statuses map[*Probe]error
	name     string
}

func (cb *controllerBase) registerBase(p *Probe) {
	cb.Lock()
	defer cb.Unlock()
	if _, ok := cb.statuses[p]; ok {
		log.Debugf("%s is already registered to %s", p, cb.name)
		return
	}
	cb.statuses[p] = errUnregistered
}

func (cb *controllerBase) statusLocked() (err error) {
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

func (cb *controllerBase) update(p *Probe, newStatus error) error {
	cb.Lock()
	defer cb.Unlock()
	prev := cb.statusLocked()
	if stat, ok := cb.statuses[p]; !ok || stat == newStatus {
		return prev
	}
	cb.statuses[p] = newStatus
	curr := cb.statusLocked()
	if prev == nil && curr != nil {
		log.Errorf("%s turns unavailable: %v", cb.name, curr)
	} else if prev != nil {
		log.Debugf("%s error status: %v", cb.name, curr)
	}
	return curr
}

func (cb *controllerBase) Close() error {
	cb.Lock()
	defer cb.Unlock()
	cb.statuses = map[*Probe]error{}
	return nil
}

type fileController struct {
	controllerBase
	path string
}

// NewFileController creates a new Controller implementation which creates a file
// at the specified path only when the registered emitters are all available.
func NewFileController(path string) Controller {
	name := filepath.Base(path)
	return &fileController{
		controllerBase: controllerBase{
			statuses: map[*Probe]error{},
			name:     name,
		},
		path: path,
	}
}

func (fc *fileController) Close() error {
	fc.controllerBase.Close()
	if _, err := os.Stat(fc.path); os.IsNotExist(err) {
		return nil
	}
	return os.Remove(fc.path)
}

func (fc *fileController) register(p *Probe, initial error) {
	fc.registerBase(p)
	fc.onChange(p, initial)
}

func (fc *fileController) onChange(p *Probe, newStatus error) {
	curr := fc.update(p, newStatus)
	var existed bool
	if _, err := os.Stat(fc.path); err == nil {
		existed = true
	}
	if curr == nil && !existed {
		f, err := os.Create(fc.path)
		if err != nil {
			log.Errorf("Failed to create the path %s: %v", fc.path, err)
			return
		}
		f.Close()
	} else if curr != nil && existed {
		if err := os.Remove(fc.path); err != nil {
			log.Errorf("Failed to remove the path %s: %v", fc.path, err)
		}
	}
}
