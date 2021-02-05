// +build integ
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

package common

import (
	"path/filepath"
	"time"

	"istio.io/istio/pkg/test/env"
)

const (
	IstioNamespace    = "istio-system"
	OperatorNamespace = "istio-operator"
	RetryDelay        = time.Second
	RetryTimeOut      = 20 * time.Minute
	NsDeletionTimeout = 5 * time.Minute
)

// ManifestPath is path of local manifests which istioctl operator init refers to.
var ManifestPath = filepath.Join(env.IstioSrc, "manifests")
