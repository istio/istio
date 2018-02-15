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
	"strings"

	"github.com/golang/protobuf/protoc-gen-go/descriptor"
)

// packageDescriptor describes a package, which is a composition of proto files.
type packageDescriptor struct {
	baseDesc
	files        []*fileDescriptor
	name         string
	title        string
	overview     string
	homeLocation string
	frontMatter  []string
}

const (
	title       = "$title: "
	overview    = "$overview: "
	location    = "$location: "
	frontMatter = "$front_matter: "
)

func newPackageDescriptor(name string, desc []*descriptor.FileDescriptorProto) *packageDescriptor {
	p := &packageDescriptor{
		name: name,
	}

	for _, fd := range desc {
		f := newFileDescriptor(fd, p)
		p.files = append(p.files, f)

		loc := f.find(newPathVector(packagePath))

		if loc != nil {
			if p.loc == nil {
				if loc.GetLeadingComments() != "" || loc.GetTrailingComments() != "" {
					p.loc = loc
					p.file = f
				}
			}

			if loc.LeadingDetachedComments != nil {
				for _, para := range loc.LeadingDetachedComments {
					lines := strings.Split(para, "\n")
					for _, l := range lines {
						l = strings.Trim(l, " ")

						if strings.HasPrefix(l, title) {
							p.title = l[len(title):]
						} else if strings.HasPrefix(l, overview) {
							p.overview = l[len(overview):]
						} else if strings.HasPrefix(l, location) {
							p.homeLocation = l[len(location):]
						} else if strings.HasPrefix(l, frontMatter) {
							p.frontMatter = append(p.frontMatter, l[len(frontMatter):])
						}
					}
				}
			}
		}
	}

	return p
}
