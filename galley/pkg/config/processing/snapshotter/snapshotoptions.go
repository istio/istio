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

package snapshotter

import (
	"istio.io/istio/galley/pkg/config/processing/snapshotter/strategy"
	"istio.io/istio/pkg/config/schema/collection"
)

// SnapshotOptions is settings for a single snapshotImpl target.
type SnapshotOptions struct {
	Distributor Distributor

	// The group name for the snapshotImpl.
	Group string

	// The publishing strategy for the snapshotImpl.
	Strategy strategy.Instance

	// The set of collections to Snapshot.
	Collections []collection.Name
}
