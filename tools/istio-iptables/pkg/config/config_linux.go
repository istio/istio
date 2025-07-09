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

package config

import (
	"errors"

	"github.com/vishvananda/netlink"
)

// Max number of attempts to netlink api when it returns ErrDumpInterrupted
const maxAttempts = 5

// LinkByNameWithRetries calls netlink.LinkByName, retrying if necessary on ErrDumpInterrupted.
// For more details, see https://github.com/istio/istio/issues/55707
func LinkByNameWithRetries(name string) (netlink.Link, error) {
	var link netlink.Link
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		link, err = netlink.LinkByName(name)
		if err == nil || !errors.Is(err, netlink.ErrDumpInterrupted) {
			return link, err
		}
	}
	return link, err
}
