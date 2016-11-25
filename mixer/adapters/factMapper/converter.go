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

package factMapper

// converter is a simple module that maps from a set of facts to a set of labels
// based on a set of supplied mapping rules.
type converter struct {
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

// newConverter returns a new independent converter instance.
func newConverter(labelFacts map[string][]string, factLabels map[string][]string) *converter {
	return &converter{
		labelFacts:    labelFacts,
		factLabels:    factLabels,
		currentFacts:  make(map[string]string),
		currentLabels: make(map[string]string)}
}

// refreshLabels refreshes the labels having been potentially affected by the updated facts
func (c *converter) refreshLabels() {
	for _, label := range c.labelsToUpdate {
		facts := c.labelFacts[label]

		c.currentLabels[label] = ""
		for _, fact := range facts {
			value, ok := c.currentFacts[fact]
			if ok {
				c.currentLabels[label] = value
				break
			}
		}
	}
}

func (c *converter) UpdateFacts(facts map[string]string) {
	// update our known facts and build up a list of labels that need updating as a result
	c.labelsToUpdate = c.labelsToUpdate[:0]
	for fact, value := range facts {
		c.currentFacts[fact] = value

		for _, label := range c.factLabels[fact] {
			c.labelsToUpdate = append(c.labelsToUpdate, label)
		}
	}

	c.refreshLabels()
}

func (c *converter) PurgeFacts(facts []string) {
	// update our known facts and build up a list of labels that need updating as a result
	c.labelsToUpdate = c.labelsToUpdate[:0]
	for _, fact := range facts {
		delete(c.currentFacts, fact)

		for _, label := range c.factLabels[fact] {
			c.labelsToUpdate = append(c.labelsToUpdate, label)
		}
	}

	c.refreshLabels()
}

func (c *converter) GetLabels() map[string]string {
	return c.currentLabels
}

func (c *converter) Reset() {
	// Yep, you heard it right, this is the fastest way
	// to clear maps in Go. Shesh.

	for k := range c.currentFacts {
		delete(c.currentFacts, k)
	}

	for k := range c.currentLabels {
		delete(c.currentLabels, k)
	}
}

func (c *converter) Stats() (numFacts int, numLabels int) {
	return len(c.currentFacts), len(c.currentLabels)
}
