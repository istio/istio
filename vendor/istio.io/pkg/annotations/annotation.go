// Copyright 2019 Istio Authors
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

// Package annotations makes it possible to track use of resource annotations within a procress
// in order to generate documentation for these uses.
package annotations

import (
	"sort"
	"sync"

	"istio.io/pkg/log"
)

// Annotation describes a single resource annotation
type Annotation struct {
	// The name of the annotation.
	Name string

	// Description of the annotation.
	Description string

	// Hide the existence of this annotation when outputting usage information.
	Hidden bool

	// Mark this annotation as deprecated when generating usage information.
	Deprecated bool
}

var allAnnotations = make(map[string]Annotation)
var mutex sync.Mutex

// Returns a description of this process' annotations, sorted by name.
func Descriptions() []Annotation {
	mutex.Lock()
	sorted := make([]Annotation, 0, len(allAnnotations))
	for _, v := range allAnnotations {
		sorted = append(sorted, v)
	}
	mutex.Unlock()

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	return sorted
}

// Register registers a new annotation.
func Register(name string, description string) Annotation {
	a := Annotation{Name: name, Description: description}
	RegisterFull(a)

	// get what's actually been registered
	mutex.Lock()
	result := allAnnotations[name]
	mutex.Unlock()

	return result
}

// RegisterFull registers a new annotation.
func RegisterFull(a Annotation) {
	mutex.Lock()

	if old, ok := allAnnotations[a.Name]; ok {
		if a.Description != "" {
			allAnnotations[a.Name] = a // last one with a description wins if the same annotation name is registered multiple times
		}

		if old.Description != a.Description || old.Deprecated != a.Deprecated || old.Hidden != a.Hidden {
			log.Warnf("The annotation %s was registered multiple times using different metadata: %v, %v", a.Name, old, a)
		}
	} else {
		allAnnotations[a.Name] = a
	}

	mutex.Unlock()
}
