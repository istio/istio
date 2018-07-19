// Copyright 2018 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this currentFile except in compliance with the License.
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
	"fmt"
	"os"

	"github.com/golang/protobuf/protoc-gen-go/descriptor"
)

// packageDescriptor describes a package, which is a composition of proto files.
type packageDescriptor struct {
	baseDesc
	files     []*fileDescriptor
	name      string
	topMatter *frontMatter
}

func newPackageDescriptor(name string, desc []*descriptor.FileDescriptorProto, perFile bool) *packageDescriptor {
	p := &packageDescriptor{
		name: name,
	}

	for _, fd := range desc {
		f := newFileDescriptor(fd, p)
		p.files = append(p.files, f)
		loc := f.find(newPathVector(packagePath))
		// The package's file is one that documents the pkg statement.
		// The first file to do this "wins".
		if loc != nil {
			if p.loc == nil {
				if loc.GetLeadingComments() != "" || loc.GetTrailingComments() != "" {
					p.loc = loc
					p.file = f
					// Inherit only f's frontMatter, don't get title from one file
				}
			} else if !perFile {
				leading := loc.GetLeadingComments()
				trailing := loc.GetTrailingComments()
				if leading != "" || trailing != "" {
					fmt.Fprintf(os.Stderr, "WARNING: package %v has a conflicting package comment in file %v.\n",
						name, f.GetName())
					fmt.Fprintf(os.Stderr, "Previous:\n%v\n%v\nCurrent:\n%v\n%v\n", p.loc.GetLeadingComments(), p.loc.GetTrailingComments(), leading, trailing)
				}
			}
		}
	}

	return p
}
