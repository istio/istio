//  Copyright 2019 Istio Authors
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

// Package processing contains primitive building blocks for creating data processing pipelines for conversion
// purposes. The core abstractions are:
//
// Graph: An end-to-end data processing Graph that can handle incoming resource events and convert them to
// Snapshots upon request. A graph is built from other abstractions below and (currently) create a single
// snapshot, when requested.
//
// Projection: A component that provides a view of a particular type of resources, in final, enveloped form.
// Views are used by the snapshotting mechanism to collect all the resources that need to go into a particular
// snapshot. Typically there is one view for each type of resource in a pipeline. However, it is possible
// to have multiple projections, coming from multiple processing paths. The snapshotting mechanism aggregates
// these resources to create a single unified collection of items.
//
// Handler: A component that can handle incoming resource events. Handlers are entry-point interfaces for
// reacting to various resource change events. They are expected to be chained with other handling components
// to eventually prepare views of enveloped resources, ready for snapshotting.
//
// There are some shrink-wrapped components in the Pipeline model to help with the building:
//
// Dispatcher: A Handler that dispatches incoming resource events, based on the type URL, to other Handlers.
// Table: A generic tabular collection of resources of a particular typeURL, keyed by the resource FullName.
//
// A Basic Pipeline looks like this:
//
// [Pipeline]
//              => Handler(Type1) => .... => Projection(Type1)  =>
//   Dispatcher => Handler(Type2) => .... => Projection(Type2)  =>  Snapshotter
//              => Handler(Type3) => .... => Projection(Type3)  =>
//
// Most current Galley resources are handled this way. However, for other resources, there can be other, more
// complicated processing models that are available.
//
//   Dispatcher => Handler(Type1) => .... => Projection(Type1) =>
//   Dispatcher => Handler(Type1) => .... => Projection(Type2) =>  Snapshotter
//   Dispatcher => Handler(Type2) => .... => Projection(Type2) =>
//   Dispatcher => Handler(Type3) => .... => Projection(Type2) =>
//
// Here, the first and second pipelines handle the same type. Also the second, third and fourth
// pipelines produce the same type.
//
package processing
