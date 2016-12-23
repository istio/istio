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

package jsonLogger

import (
	"os"

	"istio.io/mixer/pkg/adapter"
)

type (
	aspectState struct{}
)

func newLogger(c adapter.AspectConfig) (adapter.Logger, error) {
	return &aspectState{}, nil
}

func (a *aspectState) Log(l []adapter.LogEntry) error {
	var logsErr error
	for _, le := range l {
		if err := adapter.WriteJSON(os.Stdout, le); err != nil {
			logsErr = err
			continue
		}
	}
	return logsErr
}

func (a *aspectState) Close() error { return nil }
