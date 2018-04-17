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

package main

import (
	"strings"

	"github.com/golang/protobuf/protoc-gen-go/descriptor"
)

// coreDesc is an interface abstracting the abilities shared by all descriptors
type coreDesc interface {
	packageDesc() *packageDescriptor
	fileDesc() *fileDescriptor
	qualifiedName() []string
	isHidden() bool
	class() string
	location() locationDescriptor
}

// The common data for every descriptor in the model. This implements the coreDesc interface.
type baseDesc struct {
	loc    *descriptor.SourceCodeInfo_Location
	hidden bool
	cl     string
	file   *fileDescriptor
	name   []string
}

func newBaseDesc(file *fileDescriptor, path pathVector, qualifiedName []string) baseDesc {
	loc := file.find(path)
	cl := ""
	com := ""

	if loc != nil {
		var newCom string
		com = loc.GetLeadingComments()
		if com != "" {
			cl, newCom = getClass(com)
			if cl != "" {
				clone := *loc
				clone.LeadingComments = &newCom
				loc = &clone
			}
		} else {
			com = loc.GetTrailingComments()
			if com != "" {
				cl, newCom = getClass(com)
				if cl != "" {
					clone := *loc
					clone.TrailingComments = &newCom
					loc = &clone
				}
			}
		}
	}

	return baseDesc{
		file:   file,
		loc:    loc,
		hidden: strings.Contains(com, "$hide_from_docs") || strings.Contains(com, "[#not-implemented-hide:]"),
		cl:     cl,
		name:   qualifiedName,
	}
}

const class = "$class: "

func getClass(com string) (cl string, newCom string) {
	start := strings.Index(com, class)
	if start < 0 {
		return
	}

	name := start + len(class)
	end := strings.IndexAny(com[name:], " \t\n") + start + len(class)

	if end < 0 {
		newCom = com[:start]
		cl = com[name:]
	} else {
		newCom = com[:start] + com[end:]
		cl = com[name:end]
	}

	return
}

func (bd baseDesc) packageDesc() *packageDescriptor {
	return bd.file.parent
}

func (bd baseDesc) fileDesc() *fileDescriptor {
	return bd.file
}

func (bd baseDesc) qualifiedName() []string {
	return bd.name
}

func (bd baseDesc) isHidden() bool {
	return bd.hidden
}

func (bd baseDesc) class() string {
	return bd.cl
}

func (bd baseDesc) location() locationDescriptor {
	return newLocationDescriptor(bd.loc, bd.file)
}
