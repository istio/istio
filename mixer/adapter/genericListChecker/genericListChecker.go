// Copyright 2016 Istio Authors
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
	"istio.io/mixer/pkg/adapter"
)

type (
	builder     struct{ adapter.DefaultBuilder }
	listChecker struct{ entries map[string]string }
)

var (
	name = "genericListChecker"
	desc = "Checks whether a symbol is present in a list."
	conf = &config.Params{}
)

// Register records the builders exposed by this adapter.
func Register(r adapter.Registrar) {
	r.RegisterListsBuilder(newBuilder())
}

func newBuilder() builder {
	return builder{adapter.NewDefaultBuilder(name, desc, conf)}
}

func (builder) NewListsAspect(_ adapter.Env, c adapter.Config) (adapter.ListsAspect, error) {
	return newListChecker(c.(*config.Params))
}

func newListChecker(c *config.Params) (*listChecker, error) {
	entries := make(map[string]string, len(c.ListEntries))
	for _, entry := range c.ListEntries {
		entries[entry] = entry
	}

	return &listChecker{entries: entries}, nil
}

func (l *listChecker) Close() error {
	l.entries = nil
	return nil
}

func (l *listChecker) CheckList(symbol string) (bool, error) {
	_, ok := l.entries[symbol]
	return ok, nil
}
