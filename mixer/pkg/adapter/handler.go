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

package adapter

import (
	"context"
	"io"
)

type (
	// Handler represents default functionality every Adapter must implement.
	Handler interface {
		io.Closer
	}

	// HandlerBuilder represents a factory of handlers. Adapters register builders with Mixer
	// in order to allow Mixer to instantiate handlers on demand.
	//
	// For a given builder, Mixer calls the various template-specific configuration methods
	// on the handler, and once done then Mixer calls the Build method, passing a block
	// of adapter-specific configuration data as argument. The Build method returns a handler,
	// which Mixer invokes during request processing.
	HandlerBuilder interface {
		// Build must return a handler that implements all the template-specific runtime request serving
		// interfaces that the Builder was configured for.
		// This means the Handler returned by the Build method must implement all the runtime interfaces for all the
		// template the Adapter was registered for in the adapter.RegisterFn2 method.
		// If the returned Handler fails to implement the required interface that builder was registered for, mixer will
		// report an error and stop serving runtime traffic to the particular Handler.
		Build(config Config, env Env) (Handler, error)
	}

	// Builder2 represents a factory of handlers. Adapters register builders with Mixer
	// in order to allow Mixer to instantiate handlers on demand.
	//
	// For a given builder, Mixer calls the various template-specific SetXXX methods,
	// the SetAdapterConfig method, and once done then Mixer calls the Validate followed by the Build method. The Build method
	// returns a handler, which Mixer invokes during request processing.
	Builder2 interface {
		// SetAdapterConfig gives the builder the adapter-level configuration state.
		SetAdapterConfig(Config)

		// Validate is responsible for ensuring that all the configuration state given to the builder is
		// correct. The Build method is only invoked when Validate has returned success.
		Validate() *ConfigErrors

		// Build must return a handler that implements all the template-specific runtime request serving
		// interfaces that the Builder was configured for.
		// This means the Handler returned by the Build method must implement all the runtime interfaces for all the
		// template the Adapter supports.
		// If the returned Handler fails to implement the required interface that builder was registered for, Mixer will
		// report an error and stop serving runtime traffic to the particular Handler.
		Build(context.Context, Env) (Handler, error)
	}
)
