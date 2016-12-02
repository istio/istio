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

import (
	"errors"
	"strings"

	"istio.io/mixer/adapters"
)

// AdapterConfig is used to configure adapters.
type AdapterConfig struct {
	adapters.AdapterConfig

	// Rules specifies the set of label mapping rules. The keys of the map represent the
	// name of labels, while the value specifies the mapping rules to
	// turn individual fact values into label values.
	//
	// Mapping rules consist of a set of fact names separated by |. The
	// label's value is derived by iterating through all the stated facts
	// and picking the first one that is defined.
	Rules map[string]string
}

// Holds lookup tables associated with the current set of mapping rules. Thjs
// struct and its maps are immutable once created
type lookupTables struct {
	// for each label, has an ordered slice of facts that can contribute to the label
	labelFacts map[string][]string

	// for each fact that matters, has a list of the labels to update if the fact changes
	factLabels map[string][]string
}

type adapter struct {
	// active set of lookup tables. These can be swapped at any time as a result of config updates
	tables *lookupTables
}

func buildLookupTables(config *AdapterConfig) (*lookupTables, error) {
	// build our lookup tables
	labelRules := config.Rules
	labelFacts := make(map[string][]string)
	factLabels := make(map[string][]string)
	for label, rule := range labelRules {
		facts := strings.Split(rule, "|")

		// remove whitespace
		for i := range facts {
			facts[i] = strings.TrimSpace(facts[i])
			if facts[i] == "" {
				return nil, errors.New("can't have empty or whitespace fact in rule for label " + label)
			}
		}

		labelFacts[label] = facts

		for _, fact := range facts {
			factLabels[fact] = append(factLabels[fact], label)
		}
	}

	return &lookupTables{
		labelFacts: labelFacts,
		factLabels: factLabels,
	}, nil
}

// newAdapter returns a new adapter.
func newAdapter(config *AdapterConfig) (adapters.FactConverter, error) {
	tables, err := buildLookupTables(config)
	if err != nil {
		return nil, err
	}

	return &adapter{tables}, nil
}

func (a *adapter) Close() error {
	a.tables = nil
	return nil
}

func (a *adapter) NewTracker() adapters.FactTracker {
	return newTracker(a.tables)
}
