// Copyright 2016 Google Int.
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

package factMapper

// tracker is a simple module that maps from a set of facts to a set of labels
// based on a set of supplied mapping rules.
type tracker struct {
	// lookup tables derived from the current mapping rules
	tables *lookupTables

	// the current set of known facts
	currentFacts map[string]string

	// the current set of known labels, corresponding to mapping of the known facts through the selector
	currentLabels map[string]string

	// Accumulates the set of labels that need to be updated, given fact or config changes
	labelsToRefresh []string
}

// newTracker returns a new independent tracker instance.
//
// This function takes a pointer to a pointer to the lookup tables to use.
// The instance logic updates this pointer whenever new lookup tables are
// installed (which happens during config changes). The code here detects
// when this pointer changes and invalidates its locally cached state
// accordingly, such that updated labels are produced by the tracker.
func newTracker(tables *lookupTables) *tracker {
	return &tracker{
		tables:        tables,
		currentFacts:  make(map[string]string),
		currentLabels: make(map[string]string)}
}

// refreshLabels refreshes the labels in need of update
func (t *tracker) refreshLabels() {
	for _, label := range t.labelsToRefresh {
		facts := t.tables.labelFacts[label]

		t.currentLabels[label] = ""
		for _, fact := range facts {
			value, ok := t.currentFacts[fact]
			if ok {
				t.currentLabels[label] = value
				break
			}
		}
	}

	// all up to date
	t.labelsToRefresh = t.labelsToRefresh[:0]
}

func (t *tracker) UpdateFacts(facts map[string]string) {
	// update our known facts and keep track of the labels that need refreshing
	for fact, value := range facts {
		t.currentFacts[fact] = value

		for _, label := range t.tables.factLabels[fact] {
			t.labelsToRefresh = append(t.labelsToRefresh, label)
		}
	}
}

func (t *tracker) PurgeFacts(facts []string) {
	// update the known facts and keep track of the labels that need refreshing
	for _, fact := range facts {
		delete(t.currentFacts, fact)

		for _, label := range t.tables.factLabels[fact] {
			t.labelsToRefresh = append(t.labelsToRefresh, label)
		}
	}
}

func (t *tracker) GetLabels() map[string]string {
	t.refreshLabels()
	return t.currentLabels
}

func (t *tracker) Reset() {
	// nuke all the facts
	for k := range t.currentFacts {
		delete(t.currentFacts, k)
	}

	// and nuke all the labels
	for k := range t.currentLabels {
		delete(t.currentLabels, k)
	}

	t.labelsToRefresh = t.labelsToRefresh[:0]
}

func (t *tracker) Stats() (numFacts int, numLabels int) {
	return len(t.currentFacts), len(t.currentLabels)
}
