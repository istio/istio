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

// Package flow contains primitive building blocks for creating data processing pipelines for conversion
// purposes. The core abstractions are:
//
// Pipeline: An end-to-end data pipeline that can handle incoming resource events and convert them to
// Snapshots upon request. A pipeline is built from on other abstractions and (currently) create a single
// snapshot, when requested.
//
// View: A component that provides a view of a particular type of resources, in final, enveloped form. Views
// are used by the snapshotting mechanism to collect all the resources that need to go into a particular
// snapshot. Typically there is one view for each type of resource in a pipeline. However, it is possible
// to have multiple views, coming from multiple processing paths. The snapshotting mechanism aggregates
// these resources to create a single unified collection of items.
//
// Handler: A component that can handle incoming resource events. Handlers are entry-point interfaces for
// reacting to various resource change events. They are expected to be chained with other handling components
// to eventually prepare views of enveloped resources, ready for snapshotting. Handler's Handle function must
// returns true, if the event caused a change to the internal state that warrants a new snapshot.
//
// There are some shrink-wrapped components in the Pipeline model to help with the building:
//
// Dispatcher: A Handler that dispatches incoming resource events, based on the type URL, to other Handlers.
// Table: A generic tabular collection of resources of a particular typeURL, keyed by the resource FullName.
// EntryTable: A Table that stores resource.Entry types.
// Accumulator: A Handler that applies a conversion function to an incoming event and updates a destination
// Table.
//
// A Basic Pipeline looks like this:
//
// [Pipeline]
//              => Handler(Type1) => .... => View(Type1)  =>
//   Dispatcher => Handler(Type2) => .... => View(Type2)  =>  Snapshotter
//              => Handler(Type3) => .... => View(Type3)  =>
//
// Most current Galley resources are handled this way. However, for other resources, there can be other, more
// complicated pipeline models that are available.
//
//   Dispatcher => Handler(Type1) => .... => View(Type1) =>
//   Dispatcher => Handler(Type1) => .... => View(Type2) =>  Snapshotter
//   Dispatcher => Handler(Type2) => .... => View(Type2) =>
//   Dispatcher => Handler(Type3) => .... => View(Type2) =>
//
// Here, the first and second pipelines handle the same type. Also the second, third and fourth
// pipelines produce the same type.
//
package flow
