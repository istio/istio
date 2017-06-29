// Copyright 2017 Istio Authors.
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

package config

import (
	"io"

	"github.com/golang/protobuf/proto"
)

type (
	// Handler represents default functionality every Adapter must implement.
	Handler interface {
		io.Closer
	}

	// HandlerBuilder represents a factory of Handler. Adapters register builders with Mixer
	// in order to allow Mixer to instantiate Handler on demand.
	HandlerBuilder interface {
		// Build must return a Handler that must implement all the runtime request serving, template specific,
		// interfaces{} that the Builder was configured for. Mixer will pass the Handler specific configuration to the
		// Build method.
		//
		// Mixer will call the template specific configure methods on the HandlerBuilder object.
		// After the configuration is done, Mixer will call the Build method to get an instance of Handler.
		// On the returned Handler, Mixer must be should be able to call template specific processing methods
		// example ReportMetric, ReportLog etc.
		// This means the Handler returned by the Build method must implement all the runtime interfaces for all the
		// template the Adapter was registered for in the adapter.RegisterFn2 method.
		// If the returned Handler fails to implement the required interface that builder was registered for, mixer will
		// report an error and stop serving runtime traffic to the particular Handler.
		Build(cnfg proto.Message) (Handler, error)
	}
)
