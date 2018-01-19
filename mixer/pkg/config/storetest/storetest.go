// Copyright 2018 Istio Authors
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

// Package storetest provides the utility functions of config store for
// testing. Shouldn't be imported outside of the test.
package storetest

import (
	"fmt"
	"strings"

	multierror "github.com/hashicorp/go-multierror"
	"istio.io/istio/mixer/pkg/config/store"
)

// SetupStoreForTest creates an on-memory store backend, initializes its
// data with the specified specs, and returns a new store with the backend.
func SetupStoreForTest(data ...string) (store.Store, error) {
	m := store.NewMemstore()
	var errs error
	for i, d := range data {
		for j, chunk := range strings.Split(d, "\n---\n") {
			chunk = strings.TrimSpace(chunk)
			if len(chunk) == 0 {
				continue
			}
			r, err := store.ParseChunk([]byte(chunk))
			if err != nil {
				errs = multierror.Append(errs, fmt.Errorf("failed to parse at %d/%d: %v", i, j, err))
				continue
			}
			if r == nil {
				continue
			}
			m.Put(r.Key(), r)
		}
	}

	if errs != nil {
		return nil, errs
	}
	return store.WithBackend(m), nil
}
