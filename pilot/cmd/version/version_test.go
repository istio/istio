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

package version

import (
	"testing"
)

// TestVersion invokes the 'version' subcommand (used by manager, no longer by istioctl)
func TestVersion(t *testing.T) {

	// The basic version subcommand does not return an error, but we invoke it
	// anyway, to make sure it doesn't panic.
	VersionCmd.Run(VersionCmd, []string{})
}
