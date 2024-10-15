// Copyright Istio Authors
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

package file

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// markNotNeeded marks a function as 'not needed'.
// Interactions with large files can end up with a large page cache. This isn't really a big deal as the OS can reclaim
// this under memory pressure. However, Kubernetes counts page cache usage against the container memory usage.
// This leads to bloating up memory usage if we are just copying large files around.
func markNotNeeded(in *os.File) error {
	err := unix.Fadvise(int(in.Fd()), 0, 0, unix.FADV_DONTNEED)
	if err != nil {
		return fmt.Errorf("failed to mark file FADV_DONTNEED: %v", err)
	}
	return nil
}
