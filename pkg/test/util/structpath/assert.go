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

package structpath

import (
	"testing"

	"github.com/gogo/protobuf/proto"
)

type Assertable struct {
	i *Instance
	t *testing.T
}

// AssertProto similar to ForProto, but returns a fluent interface which immediately fails
// the given test when an error occurs.
func AssertProto(t *testing.T, proto proto.Message) *Assertable {
	t.Helper()
	i, err := ForProto(proto)
	if err != nil {
		t.Fatal(err)
	}
	return &Assertable{i: i, t: t}
}

func (p *Assertable) ForTest(t *testing.T) *Assertable {
	return &Assertable{i: p.i, t: t}
}

func (p *Assertable) Accept(path string, args ...interface{}) bool {
	p.t.Helper()
	err := p.i.Accept(path, args...)
	if err != nil {
		p.t.Log(err)
		return false
	}
	return true
}

func (p *Assertable) Select(path string, args ...interface{}) *Assertable {
	p.t.Helper()
	i, err := p.i.Select(path, args)
	if err != nil {
		p.t.Fatal(err)
	}
	return &Assertable{i: i, t: p.t}
}

func (p *Assertable) Equals(expected interface{}, path string, args ...interface{}) *Assertable {
	p.t.Helper()
	if err := p.i.CheckEquals(expected, path, args...); err != nil {
		p.t.Fatal(err)
	}
	return p
}

func (p *Assertable) Exists(path string, args ...interface{}) *Assertable {
	p.t.Helper()
	if err := p.i.CheckExists(path, args...); err != nil {
		p.t.Fatal(err)
	}
	return p
}

func (p *Assertable) NotExists(path string, args ...interface{}) *Assertable {
	p.t.Helper()
	if err := p.i.CheckNotExists(path, args...); err != nil {
		p.t.Fatal(err)
	}
	return p
}
