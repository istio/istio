//  Copyright 2019 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package processing_test

import (
	"time"

	"github.com/gogo/protobuf/types"

	"istio.io/istio/galley/pkg/runtime/resource"
)

var emptyInfo resource.Info
var structInfo resource.Info

func init() {
	b := resource.NewSchemaBuilder()
	emptyInfo = b.Register("empty", "type.googleapis.com/google.protobuf.Empty")
	structInfo = b.Register("struct", "type.googleapis.com/google.protobuf.Struct")
	_ = b.Build()
}

func res1V1() resource.Entry {
	return resource.Entry{
		ID: resource.VersionedKey{
			Version: "v1",
			Key: resource.Key{
				Collection: emptyInfo.Collection,
				FullName:   resource.FullNameFromNamespaceAndName("ns1", "res1"),
			},
		},
		Metadata: resource.Metadata{
			CreateTime: time.Unix(1, 1),
		},
		Item: &types.Empty{},
	}
}

func res2V1() resource.Entry {
	return resource.Entry{
		ID: resource.VersionedKey{
			Version: "v1",
			Key: resource.Key{
				Collection: emptyInfo.Collection,
				FullName:   resource.FullNameFromNamespaceAndName("ns1", "res2"),
			},
		},
		Metadata: resource.Metadata{
			CreateTime: time.Unix(2, 1),
		},
		Item: &types.Empty{},
	}
}

func res3V1() resource.Entry {
	return resource.Entry{
		ID: resource.VersionedKey{
			Version: "v1",
			Key: resource.Key{
				Collection: structInfo.Collection,
				FullName:   resource.FullNameFromNamespaceAndName("ns2", "res1"),
			},
		},
		Metadata: resource.Metadata{
			CreateTime: time.Unix(3, 1),
		},
		Item: &types.Empty{},
	}
}

func addRes1V1() resource.Event {
	return resource.Event{
		Kind:  resource.Added,
		Entry: res1V1(),
	}
}

func addRes2V1() resource.Event {
	return resource.Event{
		Kind:  resource.Added,
		Entry: res2V1(),
	}
}

func addRes3V1() resource.Event {
	return resource.Event{
		Kind:  resource.Added,
		Entry: res3V1(),
	}
}

func updateRes1V2() resource.Event {
	return resource.Event{
		Kind: resource.Updated,
		Entry: resource.Entry{
			ID: resource.VersionedKey{
				Version: "v2",
				Key: resource.Key{
					Collection: emptyInfo.Collection,
					FullName:   resource.FullNameFromNamespaceAndName("ns1", "res1"),
				},
			},
			Metadata: resource.Metadata{
				CreateTime: time.Unix(1, 2),
			},
			Item: &types.Empty{},
		},
	}
}
