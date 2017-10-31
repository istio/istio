// Copyright 2016 Istio Authors
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

import rpc "github.com/googleapis/googleapis/google/rpc"

type (
	// DenialsAspect always fail with an error.
	DenialsAspect interface {
		Aspect

		// Deny always returns an error
		Deny() rpc.Status
	}

	// DenialsBuilder builds instances of the DenyChecker aspect.
	DenialsBuilder interface {
		Builder

		// NewDenialsAspect returns a new instance of the DenyChecker aspect.
		NewDenialsAspect(env Env, c Config) (DenialsAspect, error)
	}
)
