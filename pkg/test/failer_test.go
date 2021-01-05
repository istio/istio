// Copyright Istio Authors
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

package test

import "testing"

func TestWrapper(t *testing.T) {
	t.Run("fail", func(t *testing.T) {
		if err := Wrap(func(t Failer) {
			t.Fatalf("failed")
		}); err == nil {
			t.Fatalf("expected error, got none")
		}
	})
	t.Run("success", func(t *testing.T) {
		if err := Wrap(func(t Failer) {}); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	})
	t.Run("cleanup", func(t *testing.T) {
		done := false
		if err := Wrap(func(t Failer) {
			t.Cleanup(func() {
				done = true
			})
		}); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if !done {
			t.Fatalf("cleanup not triggered")
		}
	})
}
