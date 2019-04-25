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

package scaffold

import "github.com/spf13/afero"

// CustomRenderer is the interface for writing any scaffold file that does
// not use a template.
type CustomRenderer interface {
	// SetFS sets the fs in the CustomRenderer's underlying type if it exists.
	// SetFS is used to inject the callers' fs into a CustomRenderer, which may
	// want to write/read from the same fs.
	SetFS(afero.Fs)
	// CustomRender performs arbitrary rendering of file data and returns
	// bytes to write to a file.
	CustomRender() ([]byte, error)
}
