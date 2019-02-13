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

import "testing"

// T is an interface that wraps around *testing.T. It is useful for abstracting testing.T
type T interface {
	Name() string
	Fatalf(format string, args ...interface{})
	Skipf(format string, args ...interface{})
	Helper()
	Run(name string, fn func(t *testing.T))

	asTestingT() *testing.T
}

// WrapT wraps around *testing.T. It is useful for testing the framework itself.
func WrapT(tt *testing.T) T {
	return &t{t: tt}
}

type t struct {
	t *testing.T
}

// Name implements T
func (t *t) Name() string { return t.t.Name() }

// Fatalf implements T
func (t *t) Fatalf(format string, args ...interface{}) { t.t.Fatalf(format, args...) }

// Skipf implements T
func (t *t) Skipf(format string, args ...interface{}) { t.t.Skipf(format, args...) }

// Helper implements T
func (t *t) Helper() { t.t.Helper() }

// Run implements T
func (t *t) Run(name string, fn func(t *testing.T)) { t.t.Run(name, fn) }

// asTestingT implements T
func (t *t) asTestingT() *testing.T { return t.t }
