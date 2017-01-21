// Copyright 2016 Google Inc.
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

package listChecker

import (
	"istio.io/mixer/pkg/adapter"
)

type (
	// Aspect listChecker checks given symbol against a list
	Aspect interface {
		adapter.Aspect
		// CheckList verifies whether the given symbol is on the list.
		CheckList(symbol string) (bool, error)
	}

	// Adapter builds the ListChecker Aspect
	Adapter interface {
		adapter.Adapter

		// NewListChecker returns a new ListChecker
		NewListChecker(env adapter.Env, c adapter.AspectConfig) (Aspect, error)
	}
)
