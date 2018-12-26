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

package flow

import (
	"testing"
)

func TestEnvelope_Basic(t *testing.T) {
	env, err := Envelope(addRes1V1().Entry)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if env.Metadata.Name != addRes1V1().Entry.ID.FullName.String() {
		t.Fatalf("unexpected name: %v", env.Metadata.Name)
	}

	if env.Metadata.Version != string(addRes1V1().Entry.ID.Version) {
		t.Fatalf("unexpected version: %v", env.Metadata.Version)
	}

	if env.Metadata.CreateTime == nil {
		t.Fatal("CreateTime is nil")
	}

	if env.Resource == nil {
		t.Fatal("Resource is nil is nil")
	}
}
