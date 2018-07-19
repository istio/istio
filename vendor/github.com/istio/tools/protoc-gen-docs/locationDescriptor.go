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

type locationDescriptor struct {
	*descriptor.SourceCodeInfo_Location
	file *fileDescriptor
}

func newLocationDescriptor(desc *descriptor.SourceCodeInfo_Location, file *fileDescriptor) locationDescriptor {
	return locationDescriptor{
		SourceCodeInfo_Location: desc,
		file: file,
	}
}
