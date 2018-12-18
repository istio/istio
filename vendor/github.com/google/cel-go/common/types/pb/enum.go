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

package pb

import (
	descpb "github.com/golang/protobuf/protoc-gen-go/descriptor"
)

// EnumDescription maps a qualified enum name to its numeric value.
type EnumDescription struct {
	enumName string
	file     *FileDescription
	desc     *descpb.EnumValueDescriptorProto
}

// Name of the enum.
func (ed *EnumDescription) Name() string {
	return ed.enumName
}

// Value (numeric) of the enum.
func (ed *EnumDescription) Value() int32 {
	return ed.desc.GetNumber()
}
