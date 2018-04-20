//  Copyright 2018 Istio Authors
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

package instances

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var Name = "instances"
var SingularName = "instance"
var Kind = "Instance"
var ListKind = "InstanceList"
var Version = "v1alpha2"
var Group = "config.istio.io"

var APIResource = v1.APIResource{
	Name:         Name,
	SingularName: SingularName,
	Kind:         Kind,
	Version:      Version,
	Group:        Group,
	Namespaced:   true,
}

var GroupVersion = schema.GroupVersion{
	Group:   APIResource.Group,
	Version: APIResource.Version,
}

