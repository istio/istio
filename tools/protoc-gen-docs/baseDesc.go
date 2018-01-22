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
	fileDesc() *fileDescriptor
	qualifiedName() []string
	isHidden() bool
	isExperimental() bool
	location() *descriptor.SourceCodeInfo_Location
}

// The common data for every descriptor in the model. This implements the coreDesc interface.
type baseDesc struct {
	loc          *descriptor.SourceCodeInfo_Location
	hidden       bool
	experimental bool
	file         *fileDescriptor
	name         []string
}

func newBaseDesc(file *fileDescriptor, path pathVector, qualifiedName []string) baseDesc {
	loc := file.find(path)
	experimental := false
	com := ""

	if loc != nil {
		com = loc.GetLeadingComments()
		if com != "" {
			experimental = strings.Contains(com, "$experimental")
			if experimental {
				// HACK: zap the $experimental out of the comment
				com = strings.Replace(com, "$experimental", "", 1)
				loc.LeadingComments = &com
			}
		} else {
			com = loc.GetTrailingComments()
			if com != "" {
				experimental = strings.Contains(com, "$experimental")
				if experimental {
					// HACK: zap the $experimental out of the comment
					com = strings.Replace(com, "$experimental", "", 1)
					loc.TrailingComments = &com
				}
			}
		}
	}

	return baseDesc{
		file:         file,
		loc:          loc,
		hidden:       strings.Contains(com, "$hide_from_docs") || strings.Contains(com, "[#not-implemented-hide:]"),
		experimental: experimental,
		name:         qualifiedName,
	}
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

func (bd baseDesc) isExperimental() bool {
	return bd.experimental
}

func (bd baseDesc) location() *descriptor.SourceCodeInfo_Location {
	return bd.loc
}
