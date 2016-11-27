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

// FactConversionInstance is a factory of fact converters, which
// are responsible for converting from a set of facts into a set
// of labels.
type FactConversionInstance interface {
	Instance

	// NewConverter returns a fresh converter
	NewConverter() FactConverter
}

// FactConverter maps from a set of facts to a set of labels.
type FactConverter interface {
	// UpdateFacts reports an updated set of facts. This refreshes the
	// current set of facts this converter keeps track of and determines the
	// set of labels GetLabels returns.
	UpdateFacts(facts map[string]string)

	// PurgeFacts removes a set of facts from what this converter tracks.
	PurgeFacts(facts []string)

	// GetLabels returns the current set of labels and the known facts.
	// Please note that the value returned should be treated as immutable,
	// and its content will change if new facts are reported.
	GetLabels() map[string]string

	// Reset removes all facts from this converter.
	Reset()

	// Stats returns some basic info about the converter for diagnostics.
	Stats() (numFacts int, numLabels int)
}
