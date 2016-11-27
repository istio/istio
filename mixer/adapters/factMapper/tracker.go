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
	// for each label, has an ordered slice of facts that can contribute to the label
	labelFacts map[string][]string

	// for each fact, has a list of the labels to update if the fact changes
	factLabels map[string][]string

	// the current set of known facts
	currentFacts map[string]string

	// the current set of known labels, corresponding to mapping of the known facts through the selector
	currentLabels map[string]string

	// temp buffer used in RefreshFacts
	labelsToUpdate []string
}

// newTracker returns a new independent tracker instance.
func newTracker(labelFacts map[string][]string, factLabels map[string][]string) *tracker {
	return &tracker{
		labelFacts:    labelFacts,
		factLabels:    factLabels,
		currentFacts:  make(map[string]string),
		currentLabels: make(map[string]string)}
}

// refreshLabels refreshes the labels having been potentially affected by the updated facts
func (t *tracker) refreshLabels() {
	for _, label := range t.labelsToUpdate {
		facts := t.labelFacts[label]

		t.currentLabels[label] = ""
		for _, fact := range facts {
			value, ok := t.currentFacts[fact]
			if ok {
				t.currentLabels[label] = value
				break
			}
		}
	}
}

func (t *tracker) UpdateFacts(facts map[string]string) {
	// update our known facts and build up a list of labels that need updating as a result
	t.labelsToUpdate = t.labelsToUpdate[:0]
	for fact, value := range facts {
		t.currentFacts[fact] = value

		for _, label := range t.factLabels[fact] {
			t.labelsToUpdate = append(t.labelsToUpdate, label)
		}
	}

	t.refreshLabels()
}

func (t *tracker) PurgeFacts(facts []string) {
	// update our known facts and build up a list of labels that need updating as a result
	t.labelsToUpdate = t.labelsToUpdate[:0]
	for _, fact := range facts {
		delete(t.currentFacts, fact)

		for _, label := range t.factLabels[fact] {
			t.labelsToUpdate = append(t.labelsToUpdate, label)
		}
	}

	t.refreshLabels()
}

func (t *tracker) GetLabels() map[string]string {
	return t.currentLabels
}

func (t *tracker) Reset() {
	// Yep, you heard it right, this is the fastest way
	// to clear maps in Go. Shesh.

	for k := range t.currentFacts {
		delete(t.currentFacts, k)
	}

	for k := range t.currentLabels {
		delete(t.currentLabels, k)
	}
}

func (t *tracker) Stats() (numFacts int, numLabels int) {
	return len(t.currentFacts), len(t.currentLabels)
}
