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

package krt

// OptionsBuilder is a small wrapper around KRT options to make it easy to provide a common set of options to all collections
// without excessive duplication.
type OptionsBuilder struct {
	stop     chan struct{}
	debugger *DebugHandler
}

func NewOptionsBuilder(stop chan struct{}, debugger *DebugHandler) OptionsBuilder {
	return OptionsBuilder{
		stop:     stop,
		debugger: debugger,
	}
}

// WithName applies the base options with a specific name
func (k OptionsBuilder) WithName(n string) []CollectionOption {
	return []CollectionOption{WithDebugging(k.debugger), WithStop(k.stop), WithName(n)}
}

// With applies arbitrary options along with the base options.
func (k OptionsBuilder) With(opts ...CollectionOption) []CollectionOption {
	return append([]CollectionOption{WithDebugging(k.debugger), WithStop(k.stop)}, opts...)
}

func (k OptionsBuilder) Stop() <-chan struct{} {
	return k.stop
}

func (k OptionsBuilder) Debugger() *DebugHandler {
	return k.debugger
}

// WithName allows explicitly naming a controller. This is a best practice to make debugging easier.
// If not set, a default name is picked.
func WithName(name string) CollectionOption {
	return func(c *collectionOptions) {
		c.name = name
	}
}

// WithObjectAugmentation allows transforming an object into another for usage throughout the library.
// Currently this applies to things like Name, Namespace, Labels, LabelSelector, etc. Equals is not currently supported,
// but likely in the future.
// The intended usage is to add support for these fields to collections of types that do not implement the appropriate interfaces.
// The conversion function can convert to a embedded struct with extra methods added:
//
//	type Wrapper struct { Object }
//	func (w Wrapper) ResourceName() string { return ... }
//	WithObjectAugmentation(func(o any) any { return Wrapper{o.(Object)} })
func WithObjectAugmentation(fn func(o any) any) CollectionOption {
	return func(c *collectionOptions) {
		c.augmentation = fn
	}
}

// WithStop sets a custom stop channel so a collection can be terminated when the channel is closed
func WithStop(stop <-chan struct{}) CollectionOption {
	return func(c *collectionOptions) {
		c.stop = stop
	}
}

// WithDebugging enables debugging of the collection
func WithDebugging(handler *DebugHandler) CollectionOption {
	return func(c *collectionOptions) {
		c.debugger = handler
	}
}
