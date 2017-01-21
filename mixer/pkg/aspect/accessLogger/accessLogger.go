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

// Package accessLogger provides the definition for the access logger aspect
// of the Istio Mixer.
package accessLogger

import (
	"github.com/golang/protobuf/proto"
	"istio.io/mixer/pkg/aspect"
)

type (
	// Aspect is the interface for adapters that will handle access log data
	// within the mixer.
	Aspect interface {
		aspect.Aspect

		// LogAccess directs a backend adapter to process a batch of
		// access log entries derived from potentially several Report()
		// calls.
		LogAccess([]Entry) error
	}

	// Adapter is the interface for building Aspect instances for mixer
	// access logging backend adapters.
	Adapter interface {
		aspect.Adapter

		// NewAccessLogger returns a new AccessLogger implementation, based
		// on the supplied Aspect configuration for the backend.
		NewAccessLogger(env aspect.Env, config proto.Message) (Aspect, error)
	}

	// Entry defines a basic wrapper around the access log information for
	// a single log entry. It is the job of the aspect manager to produce
	// the access log data, based on the aspect configuration and pass the
	// entries to the various backend adapters.
	//
	// Examples:
	//
	// &Entry{
	// 	LogName: "logs/access_log",
	// 	Log: "127.0.0.1 - testuser [10/Oct/2000:13:55:36 -0700] "GET /test.gif HTTP/1.0" 200 2326",
	// }
	//
	// &Entry{
	//	LogName: "logs/access_log",
	//	Labels: map[string]interface{}{
	//            "source_ip": "127.0.0.1",
	//            "url": "/test.gif",
	//            "protocol": "HTTP",
	//            "response_code": 200,
	//      }
	// }
	Entry struct {
		// LogName is the name of the access log stream to which the
		// entry corresponds.
		LogName string
		// Log is the text-formatted access log entry data. It will
		// be prepared by the aspect manager, based upon aspect config.
		Log string
		// Labels is the set of key-value pairs that can be used to
		// generate a structured access log for this entry. The aspect
		// manager will populate this map based on aspect config.
		Labels map[string]interface{}
	}
)
