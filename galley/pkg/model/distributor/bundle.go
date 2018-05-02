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

package distributor

import (
	"fmt"

	"istio.io/istio/galley/pkg/api/distrib"
	"istio.io/istio/galley/pkg/model/component"
)

// Bundle represents a self-contained bundle of configuration, from which distributable fragments and manifest
// can be generated from. The distributors should try to reduce the number of calls to Generate* methods, to
// avoid causing excessive processing.
type Bundle interface {
	fmt.Stringer

	// The Destination component that this config bundle is intended for.
	Destination() component.InstanceId

	// GenerateManifest generates a distribution manifest for this bundle.
	GenerateManifest() *distrib.Manifest
	
	// GenerateFragments generates the fragments for this bundle.
	GenerateFragments() []*distrib.Fragment
}

// BundleVersion is a unique version number for the bundle. As Bundle changes, the version number is
// incremented.
type BundleVersion int64
