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

package restart

import (
	"istio.io/istio/pkg/envoy"
)

// EventType is an enumeration for the supported types of Manager events.
type EventType string

const (
	// Starting indicates that the Manager has entered the Run loop.
	Starting EventType = "Starting"

	// ShuttingDown indicates that the Manager has begun a graceful shutdown.
	ShuttingDown EventType = "ShuttingDown"

	// Stopped indicates that the Manager has exited the Run loop. The Error field may be set if an
	// error was encountered.
	Stopped EventType = "Stopped"

	// EnvoyStarting indicates that an envoy.Instance is being started by the Manager. The Envoy field will
	// be set in the event.
	EnvoyStarting EventType = "EnvoyStarting"

	// EnvoyStopped indicates that an Envoy process has exited. The Envoy field will be set in the event
	// and the Error field may also be set if the process returned an error.
	EnvoyStopped EventType = "EnvoyStopped"
)

// Event for the lifecycle of an Envoy Manager
type Event struct {
	// Type of event.
	Type EventType

	// Envoy if for an Envoy lifecycle event.
	Envoy envoy.Instance

	// Error if one occurred. Only set by Stopped/EnvoyStopped.
	Error error
}

// EventHandler of events from an Envoy manager Manager
type EventHandler func(e Event)
