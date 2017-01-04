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

package aspectsupport

import (
	"istio.io/mixer/pkg/aspect"
	// while adding an aspect
	// 1. Add a method to Registry
	// 2. Update aspectManagers method to add that manager
	// list of aspects
	"istio.io/mixer/pkg/aspect/denyChecker"
	"istio.io/mixer/pkg/aspect/listChecker"
	// end list of aspects
)

// Registry -- Interface used by adapters to register themselves
type Registry interface {
	// RegisterCheckList
	RegisterCheckList(b listChecker.Adapter) error

	// RegisterDeny
	RegisterDeny(b denyChecker.Adapter) error

	// ByImpl gets an adapter by impl name
	ByImpl(impl string) (adapter aspect.Adapter, found bool)
}

// return list of aspect managers
func aspectManagers() []aspect.Manager {
	return []aspect.Manager{
		listChecker.NewManager(),
		denyChecker.NewManager(),
	}
}
