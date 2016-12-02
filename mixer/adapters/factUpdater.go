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

package adapters

// FactUpdater transforms facts processed by the mixer. An updater can
// readily inject brand new facts, can derive facts from existing ones, can
// replace facts, and can remove facts. All this updating happens as part of
// mixer's API pipeline, prior to the point where facts are converted into
// labels.
type FactUpdater interface {
	Adapter

	// Update is given the total current set of known facts and is free to mutate the
	// map as it wishes. It can insert new facts, delete existing facts, etc.
	Update(facts map[string]string) error
}
