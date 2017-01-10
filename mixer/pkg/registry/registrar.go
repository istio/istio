// Copyright 2017 Google Inc.
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

package registry

import (
	"istio.io/mixer/pkg/aspect/denyChecker"
	"istio.io/mixer/pkg/aspect/listChecker"
	"istio.io/mixer/pkg/aspect/logger"
	"istio.io/mixer/pkg/aspect/quota"
)

// Registrar -- Interface used by adapters to register themselves
type Registrar interface {
	// RegisterCheckList
	RegisterCheckList(listChecker.Adapter) error

	// RegisterDeny
	RegisterDeny(denyChecker.Adapter) error

	// RegisterLogger informs the mixer that an implementation of the
	// logging aspect is provided by the supplied adapter. This adapter
	// will be used to build individual instances of the logger aspect
	// according to mixer config.
	RegisterLogger(logger.Adapter) error

	// RequestQuota is used by adapters to register themselves as implementing the
	// quota aspect.
	RegisterQuota(quota.Adapter) error
}
