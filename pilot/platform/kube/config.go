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

package kube

import (
	"encoding/json"

	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/meta"
	"k8s.io/client-go/pkg/apis/meta/v1"
	"k8s.io/client-go/pkg/runtime/schema"
)

// Config is the generic Kubernetes API object wrapper
type Config struct {
	v1.TypeMeta `json:",inline"`
	Metadata    api.ObjectMeta         `json:"metadata"`
	Spec        map[string]interface{} `json:"spec"`
}

// ConfigList is the generic Kubernetes API list wrapper
type ConfigList struct {
	v1.TypeMeta `json:",inline"`
	Metadata    v1.ListMeta `json:"metadata"`
	Items       []Config    `json:"items"`
}

// GetObjectKind adapts to Object
func (e *Config) GetObjectKind() schema.ObjectKind {
	return &e.TypeMeta
}

// GetObjectMeta adapts to ObjectMetaAccessor
func (e *Config) GetObjectMeta() meta.Object {
	return &e.Metadata
}

// GetObjectKind adapts to Object
func (el *ConfigList) GetObjectKind() schema.ObjectKind {
	return &el.TypeMeta
}

// GetListMeta adapts to ListMetaAccessor
func (el *ConfigList) GetListMeta() v1.List {
	return &el.Metadata
}

// The code below is used only to work around a known problem with third-party
// resources and @ugorji JSON optimized codec.
// See discussion https://github.com/kubernetes/kubernetes/issues/36120

// ConfigListCopy is a duplicate of ConfigList
type ConfigListCopy ConfigList

// ConfigCopy is a duplicate of Config
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
