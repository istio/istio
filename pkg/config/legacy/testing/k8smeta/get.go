// Copyright Istio Authors
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

package k8smeta

import (
	"fmt"

	"istio.io/istio/pkg/config/schema"
)

// Get returns the contained k8smeta.yaml file, in parsed form.
func Get() (*schema.Metadata, error) {
	b, err := Asset("k8smeta.yaml")
	if err != nil {
		return nil, err
	}

	m, err := schema.ParseAndBuild(string(b))
	if err != nil {
		return nil, err
	}

	return m, nil
}

// MustGet calls GetBasicMeta and panics if it returns and error.
func MustGet() *schema.Metadata {
	s, err := Get()
	if err != nil {
		panic(fmt.Sprintf("k8smeta.MustGet: %v", err))
	}
	return s
}
