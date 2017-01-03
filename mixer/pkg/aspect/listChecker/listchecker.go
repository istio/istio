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
	"github.com/golang/protobuf/proto"
	"istio.io/mixer/pkg/aspect"
)

type (
	// Aspect listChecker checks given symbol against a list
	Aspect interface {
		aspect.Aspect
		// CheckList verifies whether the given symbol is on the list.
		CheckList(Symbol string) (bool, error)
	}

	// Adapter builds the ListChecker Aspect
	Adapter interface {
		aspect.Adapter
		// NewAspect returns a new ListChecker
		NewAspect(cfg proto.Message) (Aspect, error)
	}
)
