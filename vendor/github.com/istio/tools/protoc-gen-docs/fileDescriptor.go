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
	"github.com/golang/protobuf/protoc-gen-go/descriptor"
)

type fileDescriptor struct {
	*descriptor.FileDescriptorProto
	parent       *packageDescriptor
	allMessages  []*messageDescriptor                               // All the messages defined in this file
	allEnums     []*enumDescriptor                                  // All the enums defined in this file
	messages     []*messageDescriptor                               // Top-level messages defined in this file
	enums        []*enumDescriptor                                  // Top-level enums defined in this file
	services     []*serviceDescriptor                               // All services defined in this file
	dependencies []*fileDescriptor                                  // Files imported by this file
	locations    map[pathVector]*descriptor.SourceCodeInfo_Location // Provenance
	topMatter    *annotations                                       // Title, overview, homeLocation, front_matter
}

func newFileDescriptor(desc *descriptor.FileDescriptorProto, parent *packageDescriptor) *fileDescriptor {
	f := &fileDescriptor{
		FileDescriptorProto: desc,
		locations:           make(map[pathVector]*descriptor.SourceCodeInfo_Location, len(desc.GetSourceCodeInfo().GetLocation())),
		parent:              parent,
	}

	// put all the locations in a map for quick lookup
	for _, loc := range desc.GetSourceCodeInfo().GetLocation() {
		if len(loc.Path) > 0 {
			pv := newPathVector(int(loc.Path[0]))
			for _, v := range loc.Path[1:] {
				pv = pv.append(int(v))
			}
			f.locations[pv] = loc
		}
	}

	path := newPathVector(messagePath)
	for i, md := range desc.MessageType {
		f.messages = append(f.messages, newMessageDescriptor(md, nil, f, path.append(i)))
	}

	path = newPathVector(enumPath)
	for i, e := range desc.EnumType {
		f.enums = append(f.enums, newEnumDescriptor(e, nil, f, path.append(i)))
	}

	path = newPathVector(servicePath)
	for i, s := range desc.Service {
		f.services = append(f.services, newServiceDescriptor(s, f, path.append(i)))
	}

	// Find title/overview/etc content in comments and store it explicitly.
	loc := f.find(newPathVector(packagePath))
	if loc != nil && loc.LeadingDetachedComments != nil {
		f.topMatter = extractAnnotations(f.GetName(), loc)
	}

	// get the transitive close of all messages and enums
	f.aggregateMessages(f.messages)
	f.aggregateEnums(f.enums)

	return f
}

func (f *fileDescriptor) find(path pathVector) *descriptor.SourceCodeInfo_Location {
	loc := f.locations[path]
	return loc
}

func (f *fileDescriptor) aggregateMessages(messages []*messageDescriptor) {
	f.allMessages = append(f.allMessages, messages...)
	for _, msg := range messages {
		f.aggregateMessages(msg.messages)
		f.aggregateEnums(msg.enums)
	}
}

func (f *fileDescriptor) aggregateEnums(enums []*enumDescriptor) {
	f.allEnums = append(f.allEnums, enums...)
}

func (f *fileDescriptor) title() string {
	if f.topMatter != nil {
		return f.topMatter.title
	}
	return ""
}

func (f *fileDescriptor) overview() string {
	if f.topMatter != nil {
		return f.topMatter.overview
	}
	return ""
}

func (f *fileDescriptor) description() string {
	if f.topMatter != nil {
		return f.topMatter.description
	}
	return ""
}

func (f *fileDescriptor) homeLocation() string {
	if f.topMatter != nil {
		return f.topMatter.homeLocation
	}
	return ""
}

func (f *fileDescriptor) frontMatter() []string {
	if f.topMatter != nil {
		return f.topMatter.frontMatter
	}
	return nil
}
