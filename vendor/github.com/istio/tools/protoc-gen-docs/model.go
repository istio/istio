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
	plugin "github.com/golang/protobuf/protoc-gen-go/plugin"
)

// model represents a resolved in-memory version of all the input protos
type model struct {
	allFilesByName map[string]*fileDescriptor
	allDescByName  map[string]coreDesc
	packages       []*packageDescriptor
}

func newModel(request *plugin.CodeGeneratorRequest, perFile bool) (*model, error) {
	m := &model{
		allFilesByName: make(map[string]*fileDescriptor, len(request.ProtoFile)),
	}

	// organize files by package
	filesByPackage := map[string][]*descriptor.FileDescriptorProto{}
	for _, pf := range request.ProtoFile {
		pkg := packageName(pf)
		slice := filesByPackage[pkg]
		filesByPackage[pkg] = append(slice, pf)
	}

	// create all the package descriptors
	var allFiles []*fileDescriptor
	for pkg, files := range filesByPackage {
		p := newPackageDescriptor(pkg, files, perFile)
		m.packages = append(m.packages, p)

		for _, f := range p.files {
			allFiles = append(allFiles, f)
			m.allFilesByName[f.GetName()] = f
		}
	}

	// prepare a map of name to descriptor
	m.allDescByName = createDescMap(allFiles)

	// resolve all type references to nice easily used pointers
	for _, f := range allFiles {
		resolveFieldTypes(f.messages, m.allDescByName)
		resolveMethodTypes(f.services, m.allDescByName)
		resolveDependencies(f, m.allFilesByName)
	}

	return m, nil
}

func packageName(f *descriptor.FileDescriptorProto) string {
	// Does the file have a package clause?
	if pkg := f.GetPackage(); pkg != "" {
		return pkg
	}

	// use the last path element of the name, with the last dotted suffix removed.

	// First, find the last element
	name := f.GetName()
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}

	// Now drop the suffix
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[0:i]
	}

	return name
}

// createDescMap builds a map from qualified names to descriptors.
// The key names for the map come from the input data, which puts a period at the beginning.
func createDescMap(files []*fileDescriptor) map[string]coreDesc {
	descMap := make(map[string]coreDesc)
	for _, f := range files {
		// The names in this loop are defined by the proto world, not us, so the
		// package name may be empty.  If so, the dotted package name of X will
		// be ".X"; otherwise it will be ".pkg.X".
		dottedPkg := "." + f.GetPackage()
		if dottedPkg != "." {
			dottedPkg += "."
		}

		for _, svc := range f.services {
			descMap[dottedPkg+dottedName(svc)] = svc
		}

		recordEnums(f.enums, descMap, dottedPkg)
		recordMessages(f.messages, descMap, dottedPkg)
		recordServices(f.services, descMap, dottedPkg)
		resolveFieldTypes(f.messages, descMap)
	}

	return descMap
}

func recordMessages(messages []*messageDescriptor, descMap map[string]coreDesc, dottedPkg string) {
	for _, msg := range messages {
		descMap[dottedPkg+dottedName(msg)] = msg

		recordMessages(msg.messages, descMap, dottedPkg)
		recordEnums(msg.enums, descMap, dottedPkg)

		for _, f := range msg.fields {
			descMap[dottedPkg+dottedName(f)] = f
		}
	}
}

func recordEnums(enums []*enumDescriptor, descMap map[string]coreDesc, dottedPkg string) {
	for _, e := range enums {
		descMap[dottedPkg+dottedName(e)] = e

		for _, v := range e.values {
			descMap[dottedPkg+dottedName(v)] = v
		}
	}
}

func recordServices(services []*serviceDescriptor, descMap map[string]coreDesc, dottedPkg string) {
	for _, s := range services {
		descMap[dottedPkg+dottedName(s)] = s

		for _, m := range s.methods {
			descMap[dottedPkg+dottedName(m)] = m
		}
	}
}

func resolveFieldTypes(messages []*messageDescriptor, descMap map[string]coreDesc) {
	for _, msg := range messages {
		for _, field := range msg.fields {
			field.typ = descMap[field.GetTypeName()]
		}
		resolveFieldTypes(msg.messages, descMap)
	}
}

func resolveMethodTypes(services []*serviceDescriptor, descMap map[string]coreDesc) {
	for _, svc := range services {
		for _, method := range svc.methods {
			method.input = descMap[method.GetInputType()].(*messageDescriptor)
			method.output = descMap[method.GetOutputType()].(*messageDescriptor)
		}
	}
}

func resolveDependencies(file *fileDescriptor, filesByName map[string]*fileDescriptor) {
	for _, desc := range file.Dependency {
		dep := filesByName[desc]
		file.dependencies = append(file.dependencies, dep)
	}
}

// dottedName returns a dotted representation of the coreDesc's name
func dottedName(o coreDesc) string {
	return strings.Join(o.qualifiedName(), ".")
}
