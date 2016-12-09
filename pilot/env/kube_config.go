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

package env

import (
	"encoding/json"

	"k8s.io/client-go/1.5/pkg/api"
	"k8s.io/client-go/1.5/pkg/api/meta"
	"k8s.io/client-go/1.5/pkg/api/unversioned"
)

// Config is the generic Kubernetes API object wrapper
type Config struct {
	unversioned.TypeMeta `json:",inline"`
	Metadata             api.ObjectMeta         `json:"metadata"`
	Data                 map[string]interface{} `json:"data"`
}

// ConfigList is the generic Kubernetes API list wrapper
type ConfigList struct {
	unversioned.TypeMeta `json:",inline"`
	Metadata             unversioned.ListMeta `json:"metadata"`
	Items                []Config             `json:"items"`
}

// GetObjectKind adapts to Object
func (e *Config) GetObjectKind() unversioned.ObjectKind {
	return &e.TypeMeta
}

// GetObjectMeta adapts to ObjectMetaAccessor
func (e *Config) GetObjectMeta() meta.Object {
	return &e.Metadata
}

// GetObjectKind adapts to Object
func (el *ConfigList) GetObjectKind() unversioned.ObjectKind {
	return &el.TypeMeta
}

// GetListMeta adapts to ListMetaAccessor
func (el *ConfigList) GetListMeta() unversioned.List {
	return &el.Metadata
}

// The code below is used only to work around a known problem with third-party
// resources and @ugorji. If/when these issues are resolved, the code below
// should no longer be required.

type ConfigListCopy ConfigList
type ConfigCopy Config

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
