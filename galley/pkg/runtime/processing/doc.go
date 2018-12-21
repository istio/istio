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

// Package processing contains primitive building blocks for creating data processing pipelines for conversion
// purposes. The core abstractions are:
//
// Pipeline: An end-to-end pipeline that can handle incoming resource events and convert them to Snapshots upon
// request.
// Handler: A component that cand handle incoming resource events.
// View: A component that provides a view of a particular type of resources, in final, enveloped form.
// Snapshotter: A component that create a snapshot out of views.
// Collection: A generic collection that store entries, and versions by resource.FullName.
package processing
