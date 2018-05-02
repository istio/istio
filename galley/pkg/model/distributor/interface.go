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

package distributor

// Interface to be implemented by a distribution provider.
type Interface interface {

	// Initialize the distributor interface. After this call, the implementation should be ready to start
	// distributing bundles of configuration.
	Initialize() error

	// Distribute the given bundle.
	Distribute(b Bundle)

	// Shutdown the distribution interface.  The distribution system should stop distributing configuration,
	// prior to graceful shutdown.
	Shutdown()
}
