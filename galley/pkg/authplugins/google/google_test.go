// Copyright 2019 Istio Authors
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

package google

import (
	"context"
	"testing"

	"golang.org/x/oauth2/google"
)

func TestInfo(t *testing.T) {
	i := GetInfo()
	if i.Name == "" {
		t.Error("Name not valid")
	}
	if i.GetAuth == nil {
		t.Error("GetAuth not valid")
	}
}

func mockDC(_ context.Context, _ ...string) (*google.Credentials, error) {
	return &google.Credentials{}, nil
}

func TestAuth(t *testing.T) {
	findDC = mockDC
	opts, err := returnAuth(nil)
	if err != nil {
		t.Errorf("Error with auth: %v", err)
	}
	if opts == nil {
		t.Error("No auth options returned")
	}
	if len(opts) != 2 {
		t.Error("Should have 2 options")
	}
}
