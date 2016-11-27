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

package adapters

// ListChecker verifies whether a particular symbol appears in a list.
//
// TODO: how does a listChecker adapter specify which label's value it
// cares about? Could be a method on the adapter, could be a method on
// the instance. Could be something else...?
type ListChecker interface {
	Instance

	// CheckList verifies whether the given symbol is on the list.
	CheckList(symbol string) bool
}
