//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package cluster

import (
	"istio.io/istio/pkg/test/framework/resource"
)

type Kind string

const (
	Kubernetes Kind = "Kubernetes"
	Aggregate  Kind = "Aggregate"
)

type Config struct {
	Kind                    Kind              `json:"kind,omitempty"`
	Name                    string            `json:"name,omitempty"`
	Network                 string            `json:"network,omitempty"`
	ControlPlaneClusterName string            `json:"controlPlaneClusterName,omitempty"`
	ConfigClusterName       string            `json:"configClusterName,omitempty"`
	Meta                    map[string]string `json:"meta,omitempty"`
}

type Factory interface {
	Kind() Kind
	With(config ...Config) Factory
	Build(Map) (resource.Clusters, error)
}
