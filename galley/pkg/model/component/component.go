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

package component

import "fmt"

// Kind of a component.
type Kind string

const (
	// MixerKind is the component kind of Mixer
	MixerKind Kind = "mixer"

	// PilotKind is the component kind of Pilot
	PilotKind Kind = "pilot"
)

// InstanceId uniquely identifies an instance of a component.
type InstanceId struct {
	// The kind of the component
	Kind Kind

	// The unique name of the component. If left empty, then it indicates all instances of a given kind.
	Name string
}

func (in InstanceId) String() string {
	name := in.Name
	if name == "" {
		name = "*"
	}
	return fmt.Sprintf("[InstanceId](%s:%s)", in.Kind, name)
}
