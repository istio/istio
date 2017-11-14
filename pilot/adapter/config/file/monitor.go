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

package file

import (
	"bytes"
	"context"
	"crypto/sha1"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/ghodss/yaml"
	"github.com/golang/glog"
	"istio.io/istio/pilot/model"
)

const (
	defaultDuration = time.Second / 2
)

var (
	supportedExtensions = map[string]bool{
		".yaml": true,
		".yml":  true,
	}

	emptyResource = &resource{}
)

// kindToMessageName converts from the k8s Kind to the protobuf message name used for storing the parsed config.
func kindToMessageName(kind string) string {
	return "istio.proxy.v1.config." + kind
}

// messageNameToKind converts from the protobuf message name to the k8s Kind.
func messageNameToKind(messageName string) string {
	parts := strings.Split(messageName, ".")
	return parts[len(parts)-1]
}

// schemaForKind looks up the Istio protobuf schema for the given k8s Kind.
func schemaForKind(kind string) (model.ProtoSchema, bool) {
	return model.IstioConfigTypes.GetByMessageName(kindToMessageName(kind))
}

// resourceMeta is the standard metadata associated with a resource.
type resourceMeta struct {
	Name        string
	Namespace   string
	Labels      map[string]string
	Annotations map[string]string
	Revision    string
}

// resource is almost identical to crd/resource.go. This is defined here
// separately because:
// - no dependencies on actual k8s libraries
// - sha1 hash field, required for to check updates
type resource struct {
	Kind       string
	APIVersion string `json:"apiVersion"`
	Metadata   resourceMeta
	Spec       map[string]interface{}
	sha        [sha1.Size]byte
}

// getConfig converts the contents of this resource into a model.Config.
func (r *resource) toConfig() (*model.Config, error) {
	schema, ok := schemaForKind(r.Kind)
	if !ok {
		return nil, fmt.Errorf("missing schema for %q", r.Kind)
	}
	spec, err := schema.FromJSONMap(r.Spec)
	if err != nil {
		return nil, err
	}

	return &model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:            schema.Type,
			Name:            r.Metadata.Name,
			Namespace:       r.Metadata.Namespace,
			Annotations:     r.Metadata.Annotations,
			Labels:          r.Metadata.Labels,
			ResourceVersion: r.Metadata.Revision,
		},
		Spec: spec,
	}, nil
}

// compareResourceIds compares the IDs (i.e. Namespace, Kind, and Name) of the two resources and returns
// 0 if a == b, -1 if a < b, and 1 if a > b. Used for sorting resource arrays.
func compareResourceIds(a, b *resource) int {
	// Compare Namespace
	if v := strings.Compare(a.Metadata.Namespace, b.Metadata.Namespace); v != 0 {
		return v
	}

	// Compare Kind
	if v := strings.Compare(a.Kind, b.Kind); v != 0 {
		return v
	}

	// Compare Name
	return strings.Compare(a.Metadata.Name, b.Metadata.Name)
}

// resources is an array of resource objects that is capable or sorting by Namespace, Kind, and Name.
type resources []*resource

func (rs resources) Len() int {
	return len(rs)
}

func (rs resources) Swap(i, j int) {
	rs[i], rs[j] = rs[j], rs[i]
}

func (rs resources) Less(i, j int) bool {
	return compareResourceIds(rs[i], rs[j]) < 0
}

func (rs resources) Sort() {
	sort.Sort(rs)
}

// Monitor will monitor a directory containing yaml files for changes, updating a ConfigStore as changes are
// detected.
type Monitor struct {
	store         model.ConfigStore
	root          string
	kinds         map[string]bool
	checkDuration time.Duration
	resources     resources
}

func (m *Monitor) checkAndUpdate() {
	// Read all of the files under the directory and sort them based on their kind, name, and namespace.
	newResources := m.readFiles()

	// Compare the new list to the previous one and detect changes.
	oldLen := len(m.resources)
	newLen := len(newResources)
	oldIndex, newIndex := 0, 0
	for oldIndex < oldLen && newIndex < newLen {
		oldResource := m.resources[oldIndex]
		newResource := newResources[newIndex]
		if v := compareResourceIds(oldResource, newResource); v < 0 {
			m.deleteConfig(oldResource)
			oldIndex++
		} else if v > 0 {
			m.createConfig(newResource)
			newIndex++
		} else {
			if oldResource.sha != newResource.sha {
				m.updateConfig(newResource)
			}
			oldIndex++
			newIndex++
		}
	}

	// Detect remaining deletions
	for ; oldIndex < oldLen; oldIndex++ {
		m.deleteConfig(m.resources[oldIndex])
	}

	// Detect remaining additions
	for ; newIndex < newLen; newIndex++ {
		m.createConfig(newResources[newIndex])
	}

	// Save the updated list.
	m.resources = newResources
}

func (m *Monitor) createConfig(r *resource) {
	cfg, err := r.toConfig()
	if err != nil {
		glog.Warningf("Failed to parse config (%m): %v", r.Metadata, err)
		return
	}

	if _, err = m.store.Create(*cfg); err != nil {
		glog.Warningf("Failed to create config (%m): %v ", r.Metadata, err)
	}
}

func (m *Monitor) updateConfig(r *resource) {
	cfg, err := r.toConfig()
	if err != nil {
		glog.Warningf("Failed to parse config (%m): %v", r.Metadata, err)
		return
	}

	// Set the resource version based on the existing resource.
	if prev, exists := m.store.Get(cfg.Type, cfg.Name, cfg.Namespace); exists {
		cfg.ResourceVersion = prev.ResourceVersion
	}

	if _, err = m.store.Update(*cfg); err != nil {
		glog.Warningf("Failed to update config (%m): %v ", r.Metadata, err)
	}
}

func (m *Monitor) deleteConfig(r *resource) {
	schema, ok := schemaForKind(r.Kind)
	if !ok {
		glog.Warningf("Missing schema for %q ", r.Kind)
		return
	}

	if err := m.store.Delete(schema.Type, r.Metadata.Name, r.Metadata.Namespace); err != nil {
		glog.Warningf("Failed to delete config (%m): %v ", r.Metadata, err)
	}
}

func (m *Monitor) readFiles() resources {
	var result resources

	err := filepath.Walk(m.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !supportedExtensions[filepath.Ext(path)] || (info.Mode()&os.ModeType) != 0 {
			return nil
		}
		data, err := ioutil.ReadFile(path)
		if err != nil {
			glog.Warningf("Failed to read %m: %v", path, err)
			return err
		}
		for _, r := range parseYaml(data) {
			if !m.kinds[r.Kind] {
				// Ignore unsupported kinds
				continue
			}
			result = append(result, r)
		}
		return nil
	})
	if err != nil {
		glog.Errorf("failure during filepath.Walk: %v", err)
	}

	// Sort the resources by their IDs.
	result.Sort()

	return result
}

// ParseYamlFile is a utility method that parses the given yaml file and returns all of the Istio configs. This is
// equivalent to calling ioutil.ReadPath, followed by ParseYaml.
func ParseYamlFile(path string) ([]*model.Config, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return ParseYaml(data)
}

// ParseYaml is a utility method that parses the contents of a given yaml file and returns all of the Istio configs.
func ParseYaml(data []byte) ([]*model.Config, error) {
	resources := parseYaml(data)
	if len(resources) == 0 {
		return nil, nil
	}

	configs := make([]*model.Config, len(resources))
	for i, r := range resources {
		var err error
		if configs[i], err = r.toConfig(); err != nil {
			return nil, err
		}
	}

	return configs, nil
}

// parseYaml parses the data for an entire yaml file and returns slices for individual resources.
func parseYaml(data []byte) []*resource {
	if bytes.HasPrefix(data, []byte("---\n")) {
		data = data[4:]
	}
	if bytes.HasSuffix(data, []byte("\n")) {
		data = data[:len(data)-1]
	}
	if bytes.HasSuffix(data, []byte("\n---")) {
		data = data[:len(data)-4]
	}
	if len(data) == 0 {
		return nil
	}
	chunks := bytes.Split(data, []byte("\n---\n"))
	resources := make([]*resource, 0, len(chunks))
	for i, chunk := range chunks {
		r, err := parseResource(chunk)
		if err != nil {
			glog.Errorf("Error processing config [%d]: %v", i, err)
			continue
		}
		if r == nil {
			continue
		}
		resources = append(resources, r)
	}
	return resources
}

// parseResource parses a subsection of yaml for a single resource.
func parseResource(data []byte) (*resource, error) {
	r := &resource{}
	if err := yaml.Unmarshal(data, r); err != nil {
		return nil, err
	}
	if empty(r) {
		// Can be empty if no data or if it just contains comments.
		return nil, nil
	}
	if r.Kind == "" && r.Metadata.Namespace == "" && r.Metadata.Name == "" {
		return nil, fmt.Errorf("key elements are empty.\n <<%s>>", string(data))
	}
	r.sha = sha1.Sum(data)
	return r, nil
}

// Check if the parsed resource is empty
func empty(r *resource) bool {
	return reflect.DeepEqual(*r, *emptyResource)
}

// NewMonitor creates a new config store that monitors files under the given root directory for changes to config.
// If no kinds are provided (nil or empty), all IstioConfigTypes will be allowed.
func NewMonitor(delegateStore model.ConfigStore, rootDirectory string, types []string) *Monitor {
	monitor := &Monitor{
		store:         delegateStore,
		root:          rootDirectory,
		kinds:         map[string]bool{},
		checkDuration: defaultDuration,
	}

	if len(types) == 0 {
		types = model.IstioConfigTypes.Types()
	}

	for _, k := range types {
		if schema, ok := model.IstioConfigTypes.GetByType(k); ok {
			monitor.kinds[messageNameToKind(schema.MessageName)] = true
		}
	}

	return monitor
}

// Start launches a thread that monitors files under the root directory, updating the underlying config
// store when changes are detected.
func (m *Monitor) Start(ctx context.Context) {
	m.checkAndUpdate()
	tick := time.NewTicker(m.checkDuration)
	go func() {
		for {
			select {
			case <-ctx.Done():
				tick.Stop()
				return
			case <-tick.C:
				m.checkAndUpdate()
			}
		}
	}()
}
