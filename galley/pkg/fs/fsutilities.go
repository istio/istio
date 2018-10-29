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

package fs

import (
	"bytes"
	"crypto/sha1"

	"github.com/ghodss/yaml"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type istioResource struct {
	u   *unstructured.Unstructured
	sha [sha1.Size]byte
	key string
}

type fileResourceKey struct {
	key  string
	kind string
}

func parseFile(path string, data []byte) []*istioResource {
	chunks := bytes.Split(data, []byte("\n---\n"))
	resources := make([]*istioResource, 0, len(chunks))
	for i, chunk := range chunks {
		chunk = bytes.TrimSpace(chunk)
		if len(chunk) == 0 {
			continue
		}
		u, err := parseChunk(chunk)
		if err != nil {
			scope.Errorf("Error processing %s[%d]: %v", path, i, err)
			continue
		}
		if u == nil {
			continue
		}
		var key string
		if len(u.GetNamespace()) > 0 {
			key = u.GetNamespace() + "/" + u.GetName()
		} else {
			key = u.GetName()
		}
		resources = append(resources, &istioResource{u: u, sha: sha1.Sum(chunk), key: key})
	}
	return resources
}

func parseChunk(chunk []byte) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(chunk, u); err != nil {
		return nil, err
	}
	if empty(u) {
		return nil, nil
	}
	return u, nil
}

// Check if the parsed resource is empty
func empty(r *unstructured.Unstructured) bool {
	if r.Object == nil || len(r.Object) == 0 {
		return true
	}
	return false
}
