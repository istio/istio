// Copyright 2017 Istio Authors
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

package na

import (
	"reflect"
	"testing"

	"google.golang.org/grpc"
)

const (
	token = "abcdef"
)

// mockTokenFetcher implements the mock token fetcher.
type mockTokenFetcher struct {
}

// A mock fetcher for FetchToken.
func (fetcher *mockTokenFetcher) FetchToken() (string, error) {
	return token, nil
}

func TestGetDialOptions(t *testing.T) {
	gcp := gcpPlatformImpl{&mockTokenFetcher{}}
	config := &Config{RootCACertFile: "testdata/cert-chain-good.pem"}
	options, err := gcp.GetDialOptions(config)
	if err != nil {
		t.Error(err)
	}

	// Make sure there're two dial options, one for TLS and one for JWT.
	if len(options) != 2 {
		t.Errorf("Wrong dial options size")
	}

	expectedOption := grpc.WithPerRPCCredentials(&jwtAccess{token})
	if reflect.ValueOf(options[0]).Pointer() != reflect.ValueOf(expectedOption).Pointer() {
		t.Errorf("Wrong option found")
	}
}
