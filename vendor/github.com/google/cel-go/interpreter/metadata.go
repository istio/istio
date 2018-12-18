// Copyright 2018 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package interpreter

import (
	"github.com/google/cel-go/common"
)

// Metadata interface for accessing position information about expressions.
// TODO(jimlarson) Replace with common.Source.
type Metadata interface {
	// IDOffset returns raw character offset of an expression within
	// Source, or false if the expression cannot be found.
	IDOffset(exprID int64) (int32, bool)

	// IDLocation returns a common.Location for the given expression id,
	// or false if one cannot be found.  It behaves as the obvious
	// composition of IdOffset() and OffsetLocation().
	IDLocation(exprID int64) (common.Location, bool)
}
