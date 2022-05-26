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

package wasm

import (
	"time"

	"istio.io/istio/pkg/util/sets"
)

type Options struct {
	PurgeInterval         time.Duration
	ModuleExpiry          time.Duration
	InsecureRegistries    sets.Set
	HTTPRequestTimeout    time.Duration
	HTTPRequestMaxRetries int
}

func defaultOptions() Options {
	return Options{
		PurgeInterval:         1 * time.Hour,
		ModuleExpiry:          24 * time.Hour,
		InsecureRegistries:    sets.New(),
		HTTPRequestTimeout:    15 * time.Second,
		HTTPRequestMaxRetries: 5,
	}
}
