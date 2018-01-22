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

package store

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/ghodss/yaml"

	"istio.io/istio/pkg/log"
)

const defaultDuration = time.Second / 2

var supportedExtensions = map[string]bool{
	".yaml": true,
	".yml":  true,
}

// resource is almost identical to crd/resource.go. This is defined here
// separately because:
// - no dependencies on actual k8s libraries
// - sha1 hash field, required for fsstore to check updates
type resource struct {
	*BackEndResource
	sha [sha1.Size]byte
}

func (r *resource) UnmarshalJSON(bytes []byte) error {
	return json.Unmarshal(bytes, &r.BackEndResource)
}

// fsStore is StoreBackend implementation using filesystem.
type fsStore struct {
	Memstore
	root          string
	kinds         map[string]bool
	checkDuration time.Duration
	shas          map[Key][sha1.Size]byte
}

var _ Backend = &fsStore{}

// parseFile parses the data and returns as a slice of resources. "path" is only used
// for error reporting.
func parseFile(path string, data []byte) []*resource {
	chunks := bytes.Split(data, []byte("\n---\n"))
	resources := make([]*resource, 0, len(chunks))
	for i, chunk := range chunks {
		chunk = bytes.TrimSpace(chunk)
		if len(chunk) == 0 {
			continue
		}
		r, err := ParseChunk(chunk)
		if err != nil {
			log.Errorf("Error processing %s[%d]: %v", path, i, err)
			continue
		}
		if r == nil {
			continue
		}
		resources = append(resources, &resource{BackEndResource: r, sha: sha1.Sum(chunk)})
	}
	return resources
}

// ParseChunk parses a YAML formatted bytes into a BackEndResource.
func ParseChunk(chunk []byte) (*BackEndResource, error) {
	r := &BackEndResource{}
	if err := yaml.Unmarshal(chunk, r); err != nil {
		return nil, err
	}
	if empty(r) {
		// can be empty because
		// There is just white space
		// There are just comments
		return nil, nil
	}
	if r.Kind == "" || r.Metadata.Namespace == "" || r.Metadata.Name == "" {
		return nil, fmt.Errorf("key elements are empty. Extracted as %s from\n <<%s>>", r.Key(), string(chunk))
	}
	return r, nil
}

var emptyResource = &BackEndResource{}

// Check if the parsed resource is empty
func empty(r *BackEndResource) bool {
	return reflect.DeepEqual(*r, *emptyResource)
}

func (s *fsStore) readFiles() map[Key]*resource {
	result := map[Key]*resource{}

	err := filepath.Walk(s.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !supportedExtensions[filepath.Ext(path)] || (info.Mode()&os.ModeType) != 0 {
			return nil
		}
		data, err := ioutil.ReadFile(path)
		if err != nil {
			log.Warnf("Failed to read %s: %v", path, err)
			return err
		}
		for _, r := range parseFile(path, data) {
			k := r.Key()
			if !s.kinds[k.Kind] {
				continue
			}
			result[r.Key()] = r
		}
		return nil
	})
	if err != nil {
		log.Errorf("failure during filepath.Walk: %v", err)
	}
	return result
}

func (s *fsStore) checkAndUpdate() {
	newData := s.readFiles()
	updated := []Key{}
	removed := map[Key]bool{}
	for k := range s.data {
		removed[k] = true
	}
	for k, r := range newData {
		oldSha, ok := s.shas[k]
		s.shas[k] = r.sha
		if !ok {
			updated = append(updated, k)
			continue
		}
		delete(removed, k)
		if r.sha != oldSha {
			updated = append(updated, k)
		}
	}
	if len(updated) == 0 && len(removed) == 0 {
		return
	}
	for _, k := range updated {
		s.Put(k, newData[k].BackEndResource)
	}
	for k := range removed {
		s.Delete(k)
	}
}

// newFsStore creates a new StoreBackend backed by the filesystem.
func newFsStore(root string) Backend {
	return &fsStore{
		// Not using newMemstore to avoid access of MemstoreWriter for fsstore2.
		Memstore:      Memstore{data: map[Key]*BackEndResource{}},
		root:          root,
		kinds:         map[string]bool{},
		checkDuration: defaultDuration,
		shas:          map[Key][sha1.Size]byte{},
	}
}

// Init implements StoreBackend interface.
func (s *fsStore) Init(ctx context.Context, kinds []string) error {
	for _, k := range kinds {
		s.kinds[k] = true
	}
	s.checkAndUpdate()
	go func() {
		tick := time.NewTicker(s.checkDuration)
		for {
			select {
			case <-ctx.Done():
				tick.Stop()
				return
			case <-tick.C:
				s.checkAndUpdate()
			}
		}
	}()
	return nil
}
