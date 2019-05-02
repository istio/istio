/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package parse

import (
	"k8s.io/gengo/generator"
	"k8s.io/gengo/namer"
	"k8s.io/gengo/parser"
)

// NewContext returns a new Context from the builder
func NewContext(p *parser.Builder) (*generator.Context, error) {
	return generator.NewContext(p, NameSystems(), DefaultNameSystem())
}

// DefaultNameSystem returns public by default.
func DefaultNameSystem() string {
	return "public"
}

// NameSystems returns the name system used by the generators in this package.
// e.g. black-magic
func NameSystems() namer.NameSystems {
	return namer.NameSystems{
		"public": namer.NewPublicNamer(1),
		"raw":    namer.NewRawNamer("", nil),
	}
}
