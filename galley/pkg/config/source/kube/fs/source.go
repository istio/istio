// Copyright 2019 Istio Authors
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

package fs

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"istio.io/pkg/appsignals"

	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/meta/schema"
	"istio.io/istio/galley/pkg/config/scope"
	"istio.io/istio/galley/pkg/config/source/kube/inmemory"
)

var (
	supportedExtensions = map[string]bool{
		".yaml": true,
		".yml":  true,
	}
)

var nameDiscriminator int64

type source struct {
	mu               sync.Mutex
	name             string
	s                *inmemory.KubeSource
	root             string
	done             chan struct{}
	watchConfigFiles bool
}

var _ event.Source = &source{}

// New returns a new filesystem based processor.Source.
func New(root string, resources schema.KubeResources, watchConfigFiles bool) (event.Source, error) {
	src := inmemory.NewKubeSource(resources)
	name := fmt.Sprintf("fs-%d", nameDiscriminator)
	nameDiscriminator++

	s := &source{
		name:             name,
		root:             root,
		s:                src,
		watchConfigFiles: watchConfigFiles,
	}

	return s, nil
}

// Start implements processor.Source
func (s *source) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.done != nil {
		return
	}
	done := make(chan struct{})
	s.done = done

	c := make(chan appsignals.Signal, 1)
	appsignals.Watch(c)
	shut := make(chan os.Signal, 1)
	if s.watchConfigFiles {
		if err := appsignals.FileTrigger(s.root, syscall.SIGUSR1, shut); err != nil {
			scope.Source.Errorf("Unable to setup FileTrigger for %s: %v", s.root, err)
		}
	}
	go func() {
		s.reload()
		s.s.Start()
		for {
			select {
			case trigger := <-c:
				if trigger.Signal == syscall.SIGUSR1 {
					scope.Source.Infof("[%s] Triggering reload in response to: %v", s.name, trigger.Source)
					s.reload()
				}
			case <-done:
				if s.watchConfigFiles {
					shut <- syscall.SIGTERM
				}
				return
			}
		}
	}()
}

// Stop implements processor.Source.
func (s *source) Stop() {
	scope.Source.Debugf("fs.Source.Stop >>>")
	defer scope.Source.Debugf("fs.Source.Stop <<<")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done == nil {
		return
	}
	close(s.done)
	s.s.Stop()
	s.s.Clear()
	s.done = nil
}

// Dispatch implements event.Source
func (s *source) Dispatch(h event.Handler) {
	s.s.Dispatch(h)
}

func (s *source) reload() {
	s.mu.Lock()
	defer s.mu.Unlock()

	scope.Source.Debugf("[%s] Begin reloading files...", s.name)
	names := s.s.ContentNames()

	err := filepath.Walk(s.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if mode := info.Mode() & os.ModeType; !supportedExtensions[filepath.Ext(path)] || (mode != 0 && mode != os.ModeSymlink) {
			return nil
		}

		scope.Source.Infof("[%s] Discovered file: %q", s.name, path)

		data, err := ioutil.ReadFile(path)
		if err != nil {
			scope.Source.Infof("[%s] Error reading file %q: %v", s.name, path, err)
			return err
		}

		if err := s.s.ApplyContent(path, string(data)); err != nil {
			scope.Source.Errorf("[%s] Error applying file contents(%q): %v", s.name, path, err)
		}
		delete(names, path)
		return nil
	})

	if err != nil {
		scope.Source.Errorf("Error walking path during reload: %v", err)
		return
	}

	for n := range names {
		scope.Source.Infof("Removing the contents of the file %q", n)

		s.s.RemoveContent(n)
	}

	scope.Source.Debugf("[%s] Completed reloading files...", s.name)
}
