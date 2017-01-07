// Copyright 2017 Google Inc.
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

package logger

import (
	"github.com/golang/protobuf/proto"
	"istio.io/mixer/pkg/aspect"
)

type (
	// Aspect is the interface for adapters that will handle logs data
	// within the mixer.
	Aspect interface {
		aspect.Aspect

		// Log directs a backend adapter to process a batch of
		// log entries derived from potentially several Report() calls.
		Log([]Entry) error
	}

	// Entry is a set of key-value pairs of attributes that together
	// constitute the data for log entry. The key is the attribute name,
	// and the value is a typed value for the named attribute.
	Entry map[string]interface{}

	// Adapter is the interface for building Aspect instances for mixer
	// logging backends.
	Adapter interface {
		aspect.Adapter

		// NewAspect returns a new Logger implementation, based on the
		// supplied Aspect configuration for the backend.
		NewAspect(env aspect.Env, config proto.Message) (Aspect, error)
	}
)
