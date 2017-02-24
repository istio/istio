// Copyright 2017 Istio Authors
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

package test

import "github.com/golang/protobuf/ptypes/struct"

// StructMap is shorthand for a map from string to a structpb.Value pointer.
type StructMap map[string]*structpb.Value

// NewStringVal returns a new structpb.Value pointer for a string value.
func NewStringVal(s string) *structpb.Value {
	return &structpb.Value{Kind: &structpb.Value_StringValue{StringValue: s}}
}

// NewStruct returns a new structpb.Struct pointer based on the supplied fields
func NewStruct(fields map[string]*structpb.Value) *structpb.Struct {
	return &structpb.Struct{Fields: fields}
}
