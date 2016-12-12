// Copyright 2016 Google Inc.
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

package model

import (
	"fmt"
	"regexp"

	"github.com/golang/protobuf/proto"
)

// ConfigKey is the identity of the configuration object
type ConfigKey struct {
	// Config object kind, e.g.  "MyKind"
	Kind string
	// Config object name, e.g. "my-name"
	Name string
	// Config version, e.g. "prod", "canary", "v1".
	// "default" version is reserved, use ""
	Version string
}

// Config object holds the normalized config objects defined by Kind schema
type Config struct {
	ConfigKey
	// Content holds the configuration struct
	Content interface{}
}

// Registry of the configuration objects
type Registry interface {
	Get(key ConfigKey) (*Config, bool)
	Delete(key ConfigKey)
	Put(obj Config) error
	List(kind string) []*Config
}

// KindMap defines bijection between Kind and proto message name
type KindMap map[string]ProtoValidator

// ProtoValidator provides custom validation checks
type ProtoValidator struct {
	// MessageName refers to the protobuf message type name
	MessageName string
	// Description of the configuration type
	Description string
	// Validate configuration as a protobuf message
	Validate func(o proto.Message) error
}

var (
	kindRegexp    = regexp.MustCompile("^[a-zA-Z0-9]*$")
	nameRegexp    = regexp.MustCompile("^[a-z0-9]([-a-z0-9]*[a-z0-9])?$")
	versionRegexp = regexp.MustCompile("^[a-z0-9]*$")
)

// Validate names in the config key
func (k *ConfigKey) Validate() error {
	if !kindRegexp.MatchString(k.Kind) {
		return fmt.Errorf("Invalid kind: %q", k.Kind)
	}
	if !nameRegexp.MatchString(k.Name) {
		return fmt.Errorf("Invalid name: %q", k.Name)
	}
	if !versionRegexp.MatchString(k.Version) {
		return fmt.Errorf("Invalid version: %q", k.Version)
	}
	if k.Version == "default" {
		return fmt.Errorf("Version \"default\" is reserved, please use \"\"")
	}
	return nil
}

// Validate mapping
func (km KindMap) Validate() error {
	for k, v := range km {
		if !kindRegexp.MatchString(k) {
			return fmt.Errorf("Invalid kind: %q", k)
		}
		if proto.MessageType(v.MessageName) == nil {
			return fmt.Errorf("Cannot find proto message type: %q", v.MessageName)
		}
	}
	return nil
}

// ValidateConfig object
func (km KindMap) ValidateConfig(obj Config) error {
	if err := obj.ConfigKey.Validate(); err != nil {
		return err
	}
	if obj.Content == nil {
		return fmt.Errorf("Want a proto message, received empty content")
	}
	v, ok := obj.Content.(proto.Message)
	if !ok {
		return fmt.Errorf("Cannot cast to a proto message")
	}
	t, ok := km[obj.Kind]
	if !ok {
		return fmt.Errorf("Undeclared kind: %q", obj.Kind)
	}
	if proto.MessageName(v) != t.MessageName {
		return fmt.Errorf("Mismatched message type %q and kind %q",
			proto.MessageName(v), t.MessageName)
	}
	if err := t.Validate(v); err != nil {
		return err
	}
	return nil
}
