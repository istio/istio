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

package yml

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"sync"
)

// Tracker tracks the life-cycle of Yaml based resources. It stores single-part Yaml files on disk and updates the
// files as needed, as the resources change.
type Tracker struct {
	mu sync.Mutex

	discriminator int64
	resources     map[TrackerKey]*trackerState
	dir           string
}

// TrackerKey is a key representing a tracked Yaml based resource.
type TrackerKey struct {
	group     string
	kind      string
	namespace string
	name      string
}

type trackerState struct {
	part Part
	file string
}

// NewTracker returns a new Tracker instance
func NewTracker(dir string) *Tracker {
	return &Tracker{
		resources: make(map[TrackerKey]*trackerState),
		dir:       dir,
	}
}

// Apply adds the given yamlText contents as part of a given context name. If there is an existing context
// with the given name, then a diffgram will be generated.
func (t *Tracker) Apply(yamlText string) ([]TrackerKey, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	parts, err := Parse(yamlText)
	if err != nil {
		return nil, err
	}

	var result []TrackerKey
	newKeys := make(map[TrackerKey]struct{})

	for _, p := range parts {
		key := toKey(p.Descriptor)
		result = append(result, key)
		newKeys[key] = struct{}{}

		state, found := t.resources[key]
		if found {
			if err = t.deleteFile(state.file); err != nil {
				return nil, err
			}
		} else {
			state = &trackerState{}
			t.resources[key] = state
		}
		state.file = t.generateFileName(key)
		state.part = p

		if err = t.writeFile(state.file, p.Contents); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// Delete the resources from the given yamlText
func (t *Tracker) Delete(yamlText string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	parts, err := Parse(yamlText)
	if err != nil {
		return err
	}

	for _, p := range parts {
		key := toKey(p.Descriptor)

		state, found := t.resources[key]
		if found {
			if err = t.deleteFile(state.file); err != nil {
				return err
			}

			delete(t.resources, key)
		}
	}

	return nil
}

// Clear all contents froma ll contexts.
func (t *Tracker) Clear() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, s := range t.resources {
		if err := t.deleteFile(s.file); err != nil {
			return err
		}
	}

	t.resources = make(map[TrackerKey]*trackerState)

	return nil
}

// GetFileFor returns the file that keeps the on-disk state for the given key.
func (t *Tracker) GetFileFor(k TrackerKey) string {
	t.mu.Lock()
	defer t.mu.Unlock()

	state, found := t.resources[k]
	if !found {
		return ""
	}
	return state.file
}

func (t *Tracker) writeFile(file string, contents string) error {
	return ioutil.WriteFile(file, []byte(contents), os.ModePerm)
}

func (t *Tracker) deleteFile(file string) error {
	return os.Remove(file)
}

func (t *Tracker) generateFileName(key TrackerKey) string {
	t.discriminator++
	d := t.discriminator

	name := fmt.Sprintf("%s_%s_%s_%s-%d.yaml",
		sanitize(key.group), sanitize(key.kind), sanitize(key.namespace), sanitize(key.name), d)

	return path.Join(t.dir, name)
}

func sanitize(c string) string {
	return strings.Replace(
		strings.Replace(c, "/", "", -1),
		".", "_", -1)
}

func toKey(d Descriptor) TrackerKey {
	return TrackerKey{
		group:     d.Group,
		kind:      d.Kind,
		namespace: d.Metadata.Namespace,
		name:      d.Metadata.Name,
	}
}
