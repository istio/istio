// Copyright 2018 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

/*
Package packages defines types for interpreting qualified names.
*/
package packages

import (
	"strings"
)

// Packager helps interpret qualified names.
type Packager interface {
	// Package returns the qualified package name of the packager.
	//
	// The package path may be a namespace, package, or type.
	Package() string

	// ResolveCandidateNames returns the list of possible qualified names
	// visible within the module in name resolution order.
	//
	// Name candidates are returned in order of most to least qualified in
	// order to ensure that shadowing names are encountered first.
	ResolveCandidateNames(name string) []string
}

var (
	// DefaultPackage has an empty package name.
	DefaultPackage = NewPackage("")
)

// NewPackage creates a new Packager with the given qualified package name.
func NewPackage(pkg string) Packager {
	return &defaultPackage{pkg: pkg}
}

type defaultPackage struct {
	pkg string
}

func (p *defaultPackage) Package() string {
	return p.pkg
}

// ResolveCandidateNames returns the candidates name of namespaced
// identifiers in C++ resolution order.
//
// Names which shadow other names are returned first. If a name includes a
// leading dot ('.'), the name is treated as an absolute identifier which
// cannot be shadowed.
//
// Given a package name a.b.c.M.N and a type name R.s, this will deliver in
// order a.b.c.M.N.R.s, a.b.c.M.R.s, a.b.c.R.s, a.b.R.s, a.R.s, R.s.
func (p *defaultPackage) ResolveCandidateNames(name string) []string {
	if strings.HasPrefix(name, ".") {
		return []string{name[1:]}
	}

	if p.pkg == "" {
		return []string{name}
	}

	nextPkg := p.pkg
	candidates := []string{nextPkg + "." + name}
	for i := strings.LastIndex(nextPkg, "."); i >= 0; i = strings.LastIndex(nextPkg, ".") {
		nextPkg = nextPkg[:i]
		candidates = append(candidates, nextPkg+"."+name)
	}
	return append(candidates, name)
}
