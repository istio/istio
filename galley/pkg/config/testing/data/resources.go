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
	"github.com/gogo/protobuf/types"

	"istio.io/istio/galley/pkg/config/resource"
)

var (
	// EntryN1I1V1 is a test resource.Entry
	EntryN1I1V1 = &resource.Entry{
		Metadata: resource.Metadata{
			Name:    resource.NewName("n1", "i1"),
			Version: "v1",
		},
		Item: &types.Empty{},
	}

	// EntryN1I1V1Broken is a test resource.Entry
	EntryN1I1V1Broken = &resource.Entry{
		Metadata: resource.Metadata{
			Name:    resource.NewName("n1", "i1"),
			Version: "v1",
		},
		Item: nil,
	}

	// EntryN1I1V2 is a test resource.Entry
	EntryN1I1V2 = &resource.Entry{
		Metadata: resource.Metadata{
			Name:    resource.NewName("n1", "i1"),
			Version: "v2",
		},
		Item: &types.Empty{},
	}

	// EntryN2I2V2 is a test resource.Entry
	EntryN2I2V2 = &resource.Entry{
		Metadata: resource.Metadata{
			Name:    resource.NewName("n2", "i2"),
			Version: "v2",
		},
		Item: &types.Empty{},
	}
)
