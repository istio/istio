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

package data

import (
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
)

var (
	// Collection1 is a testing collection
	Collection1 = collection.NewName("collection1")

	// Collection2 is a testing collection
	Collection2 = collection.NewName("collection2")

	// Collection3 is a testing collection
	Collection3 = collection.NewName("collection3")

	// CollectionNames of all collections in the test data.
	CollectionNames = collection.Names{Collection1, Collection2, Collection3}

	// Specs is the set of collection.Specs for all test data.
	Specs = func() collection.Specs {
		b := collection.NewSpecsBuilder()
		b.MustAdd(collection.MustNewSpec(Collection1.String(), "google.protobuf", "google.protobuf.Empty"))
		b.MustAdd(collection.MustNewSpec(Collection2.String(), "google.protobuf", "google.protobuf.Empty"))
		b.MustAdd(collection.MustNewSpec(Collection3.String(), "google.protobuf", "google.protobuf.Empty"))
		return b.Build()
	}()
)
