/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package common

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"sigs.k8s.io/yaml"
)

const (
	// Busy state defines a resource being used.
	Busy = "busy"
	// Dirty state defines a resource that needs cleaning
	Dirty = "dirty"
	// Free state defines a resource that is usable
	Free = "free"
	// Cleaning state defines a resource being cleaned
	Cleaning = "cleaning"
	// Leased state defines a resource being leased in order to make a new resource
	Leased = "leased"
	// Other is used to agglomerate unspecified states for metrics reporting
	Other = "other"
)

// UserData is a map of Name to user defined interface, serialized into a string
type UserData struct {
	sync.Map
}

// UserDataMap is the standard Map version of UserMap, it is used to ease UserMap creation.
type UserDataMap map[string]string

// LeasedResources is a list of resources name that used in order to create another resource by Mason
type LeasedResources []string

// Item interfaces for resources and configs
type Item interface {
	GetName() string
}

// Resource abstracts any resource type that can be tracked by boskos
type Resource struct {
	Type       string    `json:"type"`
	Name       string    `json:"name"`
	State      string    `json:"state"`
	Owner      string    `json:"owner"`
	LastUpdate time.Time `json:"lastupdate"`
	// Customized UserData
	UserData *UserData `json:"userdata"`
}

// ResourceEntry is resource config format defined from config.yaml
type ResourceEntry struct {
	Type  string   `json:"type"`
	State string   `json:"state"`
	Names []string `json:"names,flow"`
}

// BoskosConfig defines config used by boskos server
type BoskosConfig struct {
	Resources []ResourceEntry `json:"resources,flow"`
}

// Metric contains analytics about a specific resource type
type Metric struct {
	Type    string         `json:"type"`
	Current map[string]int `json:"current"`
	Owners  map[string]int `json:"owner"`
	// TODO: implements state transition metrics
}

// NewResource creates a new Boskos Resource.
func NewResource(name, rtype, state, owner string, t time.Time) Resource {
	return Resource{
		Name:       name,
		Type:       rtype,
		State:      state,
		Owner:      owner,
		LastUpdate: t,
		UserData:   &UserData{},
	}
}

// NewResourcesFromConfig parse the a ResourceEntry into a list of resources
func NewResourcesFromConfig(e ResourceEntry) []Resource {
	var resources []Resource
	for _, name := range e.Names {
		resources = append(resources, NewResource(name, e.Type, e.State, "", time.Time{}))
	}
	return resources
}

// UserDataFromMap returns a UserData from a map
func UserDataFromMap(m UserDataMap) *UserData {
	ud := &UserData{}
	for k, v := range m {
		ud.Store(k, v)
	}
	return ud
}

// UserDataNotFound will be returned if requested resource does not exist.
type UserDataNotFound struct {
	ID string
}

func (ud *UserDataNotFound) Error() string {
	return fmt.Sprintf("user data ID %s does not exist", ud.ID)
}

// ResourceByUpdateTime helps sorting resources by update time
type ResourceByUpdateTime []Resource

func (ut ResourceByUpdateTime) Len() int           { return len(ut) }
func (ut ResourceByUpdateTime) Swap(i, j int)      { ut[i], ut[j] = ut[j], ut[i] }
func (ut ResourceByUpdateTime) Less(i, j int) bool { return ut[i].LastUpdate.Before(ut[j].LastUpdate) }

// ResourceByName helps sorting resources by name
type ResourceByName []Resource

func (ut ResourceByName) Len() int           { return len(ut) }
func (ut ResourceByName) Swap(i, j int)      { ut[i], ut[j] = ut[j], ut[i] }
func (ut ResourceByName) Less(i, j int) bool { return ut[i].GetName() < ut[j].GetName() }

// CommaSeparatedStrings is used to parse comma separated string flag into a list of strings
type CommaSeparatedStrings []string

func (r *CommaSeparatedStrings) String() string {
	return fmt.Sprint(*r)
}

// Set parses the flag value into a CommaSeparatedStrings
func (r *CommaSeparatedStrings) Set(value string) error {
	if len(*r) > 0 {
		return errors.New("resTypes flag already set")
	}
	for _, rtype := range strings.Split(value, ",") {
		*r = append(*r, rtype)
	}
	return nil
}

// GetName implements the Item interface used for storage
func (res Resource) GetName() string { return res.Name }

// UnmarshalJSON implements JSON Unmarshaler interface
func (ud *UserData) UnmarshalJSON(data []byte) error {
	tmpMap := UserDataMap{}
	if err := json.Unmarshal(data, &tmpMap); err != nil {
		return err
	}
	ud.FromMap(tmpMap)
	return nil
}

// MarshalJSON implements JSON Marshaler interface
func (ud *UserData) MarshalJSON() ([]byte, error) {
	return json.Marshal(ud.ToMap())
}

// Extract unmarshalls a string a given struct if it exists
func (ud *UserData) Extract(id string, out interface{}) error {
	content, ok := ud.Load(id)
	if !ok {
		return &UserDataNotFound{id}
	}
	return yaml.Unmarshal([]byte(content.(string)), out)
}

// User Data are used to store custom information mainly by Mason and Masonable implementation.
// Mason used a LeasedResource keys to store information about other resources that used to
// create the given resource.

// Set marshalls a struct to a string into the UserData
func (ud *UserData) Set(id string, in interface{}) error {
	b, err := yaml.Marshal(in)
	if err != nil {
		return err
	}
	ud.Store(id, string(b))
	return nil
}

// Update updates existing UserData with new UserData.
// If a key as an empty string, the key will be deleted
func (ud *UserData) Update(new *UserData) {
	if new == nil {
		return
	}
	new.Range(func(key, value interface{}) bool {
		if value.(string) != "" {
			ud.Store(key, value)
		} else {
			ud.Delete(key)
		}
		return true
	})
}

// ToMap converts a UserData to UserDataMap
func (ud *UserData) ToMap() UserDataMap {
	m := UserDataMap{}
	ud.Range(func(key, value interface{}) bool {
		m[key.(string)] = value.(string)
		return true
	})
	return m
}

// FromMap feels updates user data from a map
func (ud *UserData) FromMap(m UserDataMap) {
	for key, value := range m {
		ud.Store(key, value)
	}
}

// ItemToResource casts a Item back to a Resource
func ItemToResource(i Item) (Resource, error) {
	res, ok := i.(Resource)
	if !ok {
		return Resource{}, fmt.Errorf("cannot construct Resource from received object %v", i)
	}
	return res, nil
}
