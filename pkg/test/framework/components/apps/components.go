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

package apps

import (
	"istio.io/istio/pkg/test/framework/components/apps/kube"
	"istio.io/istio/pkg/test/framework/components/apps/local"
)

var (
	// LocalComponent is a component for the local environment.
	LocalComponent = local.Component
	// KubeComponent is a component for the kubernetes environment.
	KubeComponent = kube.Component
)
