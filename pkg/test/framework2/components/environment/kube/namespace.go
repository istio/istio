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

package kube

import (
	"io"

	"istio.io/istio/pkg/test/framework2/resource"
	"istio.io/istio/pkg/test/kube"
)

// Namespace represents a Kubernetes namespace. It is tracked as a resource.
type Namespace struct {
	Name string
	a    *kube.Accessor
}

var _ io.Closer = &Namespace{}
var _ resource.Dumper = &Namespace{}

// Close implements io.Closer
func (n *Namespace) Close() error {
	if n.Name != "" {
		ns := n.Name
		n.Name = ""
		return n.a.DeleteNamespace(ns)
	}

	return nil
}

// Dump implements resource.Dumper
func (n *Namespace) Dump() {
	// TODO: Make this dumpable.
}