// Copyright 2019 Istio Authors
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

package resources

import "istio.io/istio/galley/pkg/runtime/resource"

var (
	// EmptyInfo resource info for a protobuf Empty
	EmptyInfo resource.Info

	// StructInfo resource info for a protobuf Struct
	StructInfo resource.Info

	// TestSchema a schema containing EmptyInfo and StructInfo
	TestSchema *resource.Schema
)

func init() {
	b := resource.NewSchemaBuilder()
	EmptyInfo = b.Register("empty", "type.googleapis.com/google.protobuf.Empty")
	StructInfo = b.Register("struct", "type.googleapis.com/google.protobuf.Struct")
	TestSchema = b.Build()
}
