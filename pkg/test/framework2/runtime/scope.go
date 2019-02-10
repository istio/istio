//  Copyright 2019 Istio Authors
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

package runtime

import (
	"io"

	"github.com/hashicorp/go-multierror"
	"istio.io/istio/pkg/test/framework2/resource"
)

// scope hold resources in a particular scope.
type scope struct {
	parent *scope

	resources []interface{}

	children []*scope
}

func newScope(p *scope) *scope {
	s := &scope{
		parent: p,
	}

	if p != nil {
		p.children = append(p.children, s)
	}

	return s
}

func (s *scope) add(r interface{}) {
	s.resources = append(s.resources, r)
}

func (s *scope) done(nocleanup bool) error {
	var err error
	if !nocleanup {
		for _, c := range s.resources {
			if closer, ok := c.(io.Closer); ok {
				if e := closer.Close(); e != nil {
					err = multierror.Append(e, err)
				}
			}
		}
	}
	s.resources = nil // set resources to nil to avoid resetting them

	if e := s.reset(); e != nil {
		err = multierror.Append(err, e)
	}

	return err
}

func (s *scope) reset() error {
	var err error
	for _, c := range s.resources {
		if r, ok := c.(resource.Resetter); ok {

			if e := r.Reset(); e != nil {
				err = multierror.Append(e, err)
			}
		}
	}

	if s.parent != nil {
		if e := s.parent.reset(); e != nil {
			err = multierror.Append(e, err)
		}
	}

	return err
}

func (s *scope) dump() {
	for _, c := range s.children {
		c.dump()
	}

	for _, c := range s.resources {
		if d, ok := c.(resource.Dumper); ok {
			d.Dump()
		}
	}
}
