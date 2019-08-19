// Copyright 2018 Istio Authors
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

package schemas

import (
	"errors"

	"github.com/gogo/protobuf/proto"

	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/test/config"
)

var (
	// MockConfig is used purely for testing
	MockConfig = schema.Instance{
		Type:        "mock-config",
		Plural:      "mock-configs",
		Group:       "test",
		Version:     "v1",
		MessageName: "test.MockConfig",
		Validate: func(name, namespace string, msg proto.Message) error {
			if msg.(*config.MockConfig).Key == "" {
				return errors.New("empty key")
			}
			return nil
		},
	}
)
