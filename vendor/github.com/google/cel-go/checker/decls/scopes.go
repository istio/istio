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

package decls

import exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"

// Scopes represents nested Decl environments.
// A Scopes structure is a stack of Groups (the highest array index
// is the top of stack), with the top of the stack being the "innermost"
// scope, and the bottom being the "outermost" scope.  Each group is a mapping
// of names to Decls in the ident and function namespaces.
// Lookups are performed such that bindings in inner scopes shadow those
// in outer scopes.
type Scopes struct {
	scopes []*Group
}

// NewScopes creates a new, empty Scopes.
// Some operations can't be safely performed until a Group is added with Push.
func NewScopes() *Scopes {
	return &Scopes{
		scopes: []*Group{},
	}
}

// Push adds an empty Group as the new innermost scope.
func (s *Scopes) Push() {
	g := newGroup()
	s.scopes = append(s.scopes, g)
}

// Pop removes the innermost Group from Scopes.
// Scopes should have at least one Group.
func (s *Scopes) Pop() {
	s.scopes = s.scopes[:len(s.scopes)-1]
}

// AddIdent adds the ident Decl in the outermost scope.
// Any previous entry for an ident of the same name is overwritten.
// Scopes must have at least one group.
func (s *Scopes) AddIdent(decl *exprpb.Decl) {
	s.scopes[0].idents[decl.Name] = decl
}

// FindIdent finds the first ident Decl with a matching name in Scopes.
// The search is performed from innermost to outermost.
// Returns nil if not such ident in Scopes.
func (s *Scopes) FindIdent(name string) *exprpb.Decl {
	for i := len(s.scopes) - 1; i >= 0; i-- {
		scope := s.scopes[i]
		if ident, found := scope.idents[name]; found {
			return ident
		}
	}
	return nil
}

// FindIdentInScope returns the named ident binding in the innermost scope.
// Returns nil if no such binding exists.
// Scopes must have at least one group.
func (s *Scopes) FindIdentInScope(name string) *exprpb.Decl {
	if ident, found := s.scopes[len(s.scopes)-1].idents[name]; found {
		return ident
	}
	return nil
}

// AddFunction adds the function Decl in the outermost scope.
// Any previous entry for a function of the same name is overwritten.
// Scopes must have at least one group.
func (s *Scopes) AddFunction(fn *exprpb.Decl) {
	s.scopes[0].functions[fn.Name] = fn
}

// FindFunction finds the first function Decl with a matching name in Scopes.
// The search is performed from innermost to outermost.
// Returns nil if no such function in Scopes.
func (s *Scopes) FindFunction(name string) *exprpb.Decl {
	for i := len(s.scopes) - 1; i >= 0; i-- {
		scope := s.scopes[i]
		if fn, found := scope.functions[name]; found {
			return fn
		}
	}
	return nil
}

// Group is a set of Decls that is pushed on or popped off a Scopes as a unit.
// Contains separate namespaces for idenifier and function Decls.
// (Should be named "Scope" perhaps?)
type Group struct {
	idents    map[string]*exprpb.Decl
	functions map[string]*exprpb.Decl
}

func newGroup() *Group {
	return &Group{
		idents:    make(map[string]*exprpb.Decl),
		functions: make(map[string]*exprpb.Decl),
	}
}
