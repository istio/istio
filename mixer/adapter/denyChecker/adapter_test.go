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

package denyChecker

import (
	"testing"

	"google.golang.org/genproto/googleapis/rpc/code"
)

func TestAll(t *testing.T) {
	b := newAdapter()

	a, err := b.NewAspect(b.DefaultConfig())
	if err != nil {
		t.Errorf("Unable to create aspect: %v", err)
	}

	status := a.Deny()
	if status.Code != int32(code.Code_FAILED_PRECONDITION) {
		t.Error("a.Deny returned %d, expected %d", status.Code, int32(code.Code_FAILED_PRECONDITION))
	}

	if err = a.Close(); err != nil {
		t.Errorf("a.Close failed: %v", err)
	}

	a, err = b.NewAspect(&Config{ErrorCode: int32(code.Code_INVALID_ARGUMENT)})
	if err != nil {
		t.Errorf("Unable to create aspect: %v", err)
	}

	status = a.Deny()
	if status.Code != int32(code.Code_INVALID_ARGUMENT) {
		t.Error("a.Deny returned %d, expected %d", status.Code, int32(code.Code_INVALID_ARGUMENT))
	}

	if err = a.Close(); err != nil {
		t.Errorf("a.Close failed: %v", err)
	}
}
