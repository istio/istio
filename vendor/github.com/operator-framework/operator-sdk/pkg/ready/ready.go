// Copyright 2018 The Operator-SDK Authors
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

package ready

import (
	"os"
)

const FileName = "/tmp/operator-sdk-ready"

// Ready holds state about whether the operator is ready and communicates that
// to a Kubernetes readiness probe.
type Ready interface {
	// Set ensures that future readiness probes will indicate that the operator
	// is ready.
	Set() error

	// Unset ensures that future readiness probes will indicate that the
	// operator is not ready.
	Unset() error
}

// NewFileReady returns a Ready that uses the presence of a file on disk to
// communicate whether the operator is ready. The operator's Pod definition
// should include a readinessProbe of "exec" type that calls
// "stat /tmp/operator-sdk-ready".
func NewFileReady() Ready {
	return fileReady{}
}

type fileReady struct{}

// Set creates a file on disk whose presence can be used by a readiness probe
// to determine that the operator is ready.
func (r fileReady) Set() error {
	f, err := os.Create(FileName)
	if err != nil {
		return err
	}
	return f.Close()
}

// Unset removes the file on disk that was created by Set().
func (r fileReady) Unset() error {
	return os.Remove(FileName)
}
