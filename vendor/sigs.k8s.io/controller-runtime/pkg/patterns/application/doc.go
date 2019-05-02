/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package application documents patterns for building Controllers to manage specific applications.
//
//
// An application is a Controller and Resource that together implement the operational logic for an application.
// They are often used to take off-the-shelf OSS applications, and make them Kubernetes native.
//
// A typical application Controller may use a new builder.SimpleController() to create a Controller
// for a single API type that manages other objects it creates.
//
// Application Controllers are most useful for stateful applications such as Cassandra, Etcd and MySQL
// which contain operation logic for sharding, backup and restore, upgrade / downgrade, etc.
package application
