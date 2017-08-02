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

package tpr

import (
	"encoding/json"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Config is the generic Kubernetes API object wrapper
type Config struct {
	meta_v1.TypeMeta `json:",inline"`
	Metadata         meta_v1.ObjectMeta     `json:"metadata"`
	Spec             map[string]interface{} `json:"spec"`
}

// ConfigList is the generic Kubernetes API list wrapper
type ConfigList struct {
	meta_v1.TypeMeta `json:",inline"`
	Metadata         meta_v1.ListMeta `json:"metadata"`
	Items            []Config         `json:"items"`
}

// GetObjectKind adapts to Object
func (e *Config) GetObjectKind() schema.ObjectKind {
	return &e.TypeMeta
}

// GetObjectMeta adapts to ObjectMetaAccessor
func (e *Config) GetObjectMeta() meta_v1.Object {
	return &e.Metadata
}

// DeepCopyObject implements a mandatory interface
func (e *Config) DeepCopyObject() runtime.Object {
	if e == nil {
		return nil
	}
	return e
}

// GetObjectKind adapts to Object
func (el *ConfigList) GetObjectKind() schema.ObjectKind {
	return &el.TypeMeta
}

// GetListMeta adapts to ListMetaAccessor
func (el *ConfigList) GetListMeta() meta_v1.List {
	return &el.Metadata
}

// DeepCopyObject implements a mandatory interface
func (el *ConfigList) DeepCopyObject() runtime.Object {
	if el == nil {
		return nil
	}
	return el
}

// The code below is used only to work around a known problem with third-party
// resources and @ugorji JSON optimized codec.
// See discussion https://github.com/kubernetes/kubernetes/issues/36120

// ConfigListCopy is an alias of ConfigList
type ConfigListCopy ConfigList

// ConfigCopy is an alias of Config
type ConfigCopy Config

// UnmarshalJSON is a workaround
func (e *Config) UnmarshalJSON(data []byte) error {
	tmp := ConfigCopy{}
	err := json.Unmarshal(data, &tmp)
	if err != nil {
		return err
	}
	tmp2 := Config(tmp)
	*e = tmp2
	return nil
}

// UnmarshalJSON is a workaround
func (el *ConfigList) UnmarshalJSON(data []byte) error {
	tmp := ConfigListCopy{}
	err := json.Unmarshal(data, &tmp)
	if err != nil {
		return err
	}
	tmp2 := ConfigList(tmp)
	*el = tmp2
	return nil
}
