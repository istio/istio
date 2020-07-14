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

package yml

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"sync"
)

// Cache tracks the life-cycle of Yaml based resources. It stores single-part Yaml files on disk and updates the
// files as needed, as the resources change.
type Cache struct {
	mu sync.Mutex

	discriminator int64
	resources     map[CacheKey]*resourceState
	dir           string
}

// CacheKey is a key representing a tracked Yaml based resource.
type CacheKey struct {
	group     string
	kind      string
	namespace string
	name      string
}

type resourceState struct {
	part Part
	file string
}

// NewCache returns a new Cache instance
func NewCache(dir string) *Cache {
	return &Cache{
		resources: make(map[CacheKey]*resourceState),
		dir:       dir,
	}
}

// Apply adds the given yamlText contents as part of a given context name. If there is an existing context
// with the given name, then a diffgram will be generated.
func (c *Cache) Apply(yamlText string) ([]CacheKey, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	parts, err := Parse(yamlText)
	if err != nil {
		return nil, err
	}

	var result []CacheKey
	newKeys := make(map[CacheKey]struct{})

	for _, p := range parts {
		key := toKey(p.Descriptor)
		result = append(result, key)
		newKeys[key] = struct{}{}

		state, found := c.resources[key]
		if found {
			if err = c.deleteFile(state.file); err != nil {
				return nil, err
			}
		} else {
			state = &resourceState{}
			c.resources[key] = state
		}
		state.file = c.generateFileName(key)
		state.part = p

		if err = c.writeFile(state.file, p.Contents); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// Delete the resources from the given yamlText
func (c *Cache) Delete(yamlText string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	parts, err := Parse(yamlText)
	if err != nil {
		return err
	}

	for _, p := range parts {
		key := toKey(p.Descriptor)

		state, found := c.resources[key]
		if found {
			if err = c.deleteFile(state.file); err != nil {
				return err
			}

			delete(c.resources, key)
		}
	}

	return nil
}

// AllKeys returns all resource keys in the tracker.
func (c *Cache) AllKeys() []CacheKey {
	c.mu.Lock()
	defer c.mu.Unlock()

	var result []CacheKey

	for k := range c.resources {
		result = append(result, k)
	}

	return result
}

// Clear all tracked yaml content.
func (c *Cache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, s := range c.resources {
		if err := c.deleteFile(s.file); err != nil {
			return err
		}
	}

	c.resources = make(map[CacheKey]*resourceState)

	return nil
}

// GetFileFor returns the file that keeps the on-disk state for the given key.
func (c *Cache) GetFileFor(k CacheKey) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	state, found := c.resources[k]
	if !found {
		return ""
	}
	return state.file
}

func (c *Cache) writeFile(file string, contents string) error {
	return ioutil.WriteFile(file, []byte(contents), os.ModePerm)
}

func (c *Cache) deleteFile(file string) error {
	return os.Remove(file)
}

func (c *Cache) generateFileName(key CacheKey) string {
	c.discriminator++
	d := c.discriminator

	name := fmt.Sprintf("%s_%s_%s_%s-%d.yaml",
		sanitize(key.group), sanitize(key.kind), sanitize(key.namespace), sanitize(key.name), d)

	return path.Join(c.dir, name)
}

func sanitize(c string) string {
	return strings.Replace(
		strings.Replace(c, "/", "", -1),
		".", "_", -1)
}

func toKey(d Descriptor) CacheKey {
	return CacheKey{
		group:     d.Group,
		kind:      d.Kind,
		namespace: d.Metadata.Namespace,
		name:      d.Metadata.Name,
	}
}
