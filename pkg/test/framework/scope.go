//  Copyright Istio Authors
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

package framework

import (
	"fmt"
	"io"
	"reflect"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

// scope hold resources in a particular scope.
type scope struct {
	// friendly name for the scope for debugging purposes.
	id string

	parent *scope

	resources []resource.Resource

	closers []io.Closer

	children []*scope

	closeChan chan struct{}

	skipDump bool

	// Mutex to lock changes to resources, children, and closers that can be done concurrently
	mu sync.Mutex
}

func newScope(id string, p *scope) *scope {
	s := &scope{
		id:        id,
		parent:    p,
		closeChan: make(chan struct{}, 1),
	}

	if p != nil {
		p.children = append(p.children, s)
	}

	return s
}

func (s *scope) add(r resource.Resource, id *resourceID) {
	scopes.Framework.Debugf("Adding resource for tracking: %v", id)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resources = append(s.resources, r)

	if c, ok := r.(io.Closer); ok {
		s.closers = append(s.closers, c)
	}
}

func (s *scope) get(ref any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	refVal := reflect.ValueOf(ref)
	if refVal.Kind() != reflect.Ptr {
		return fmt.Errorf("ref must be a pointer instead got: %T", ref)
	}
	// work with the underlying value rather than the pointer
	refVal = refVal.Elem()

	targetT := refVal.Type()
	if refVal.Kind() == reflect.Slice {
		// for slices look at the element type
		targetT = targetT.Elem()
	}

	for _, res := range s.resources {
		if res == nil {
			continue
		}
		resVal := reflect.ValueOf(res)
		if resVal.Type().AssignableTo(targetT) {
			if refVal.Kind() == reflect.Slice {
				refVal.Set(reflect.Append(refVal, resVal))
			} else {
				refVal.Set(resVal)
				return nil
			}
		}
	}

	if s.parent != nil {
		// either didn't find the value or need to continue filling the slice
		return s.parent.get(ref)
	}

	if refVal.Kind() != reflect.Slice {
		// didn't find the non-slice value
		return fmt.Errorf("no %v in context", targetT)
	}

	return nil
}

func (s *scope) addCloser(c io.Closer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closers = append(s.closers, c)
}

func (s *scope) done(nocleanup bool) error {
	scopes.Framework.Debugf("Begin cleaning up scope: %v", s.id)

	// First, wait for all of the children to be done.
	for _, c := range s.children {
		c.waitForDone()
	}

	// Upon returning, notify the parent that we're done.
	defer func() {
		close(s.closeChan)
	}()

	var err error
	// Do reverse walk for cleanup.
	for i := len(s.closers) - 1; i >= 0; i-- {
		c := s.closers[i]

		if nocleanup {
			if cc, ok := c.(*closer); ok && cc.noskip {
				continue
			} else if !ok {
				continue
			}
		}

		name := "lambda"
		if r, ok := c.(resource.Resource); ok {
			name = fmt.Sprintf("resource %v", r.ID())
		}

		scopes.Framework.Debugf("Begin cleaning up %s", name)
		if e := c.Close(); e != nil {
			scopes.Framework.Debugf("Error cleaning up %s: %v", name, e)
			err = multierror.Append(err, e).ErrorOrNil()
		}
		scopes.Framework.Debugf("Cleanup complete for %s", name)
	}

	s.mu.Lock()
	s.resources = nil
	s.closers = nil
	s.mu.Unlock()

	scopes.Framework.Debugf("Done cleaning up scope: %v", s.id)
	return err
}

func (s *scope) waitForDone() {
	<-s.closeChan
}

func (s *scope) skipDumping() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.skipDump = true
}

func (s *scope) dump(ctx resource.Context, recursive bool) {
	s.mu.Lock()
	skip := s.skipDump
	s.mu.Unlock()
	if skip {
		return
	}
	st := time.Now()
	defer func() {
		l := scopes.Framework.Debugf
		if time.Since(st) > time.Second*10 {
			// Log slow dumps at higher level
			l = scopes.Framework.Infof
		}
		l("Done dumping: %s for %s (%v)", s.id, ctx.ID(), time.Since(st))
	}()
	s.mu.Lock()
	defer s.mu.Unlock()
	if recursive {
		for _, c := range s.children {
			c.dump(ctx, recursive)
		}
	}
	wg := sync.WaitGroup{}
	for _, c := range s.resources {
		if d, ok := c.(resource.Dumper); ok {
			d := d
			wg.Add(1)
			go func() {
				d.Dump(ctx)
				wg.Done()
			}()
		}
	}
	wg.Wait()
}
