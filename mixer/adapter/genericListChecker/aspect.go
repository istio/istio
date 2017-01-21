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

package genericListChecker

import (
	"istio.io/mixer/adapter/genericListChecker/config"
	"istio.io/mixer/pkg/adapter/listChecker"
)

type aspectState struct {
	entries map[string]string
}

func newAspect(c *config.Params) (listChecker.Aspect, error) {
	entries := make(map[string]string, len(c.ListEntries))
	for _, entry := range c.ListEntries {
		entries[entry] = entry
	}

	return &aspectState{entries: entries}, nil
}

func (a *aspectState) Close() error {
	a.entries = nil
	return nil
}

func (a *aspectState) CheckList(symbol string) (bool, error) {
	_, ok := a.entries[symbol]
	return ok, nil
}
