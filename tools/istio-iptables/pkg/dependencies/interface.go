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

package dependencies

import (
	"io"

	"istio.io/istio/tools/istio-iptables/pkg/constants"
)

// Dependencies is used as abstraction for the commands used from the operating system
type Dependencies interface {
	// Run runs a command
	Run(cmd constants.IptablesCmd, iptVer *IptablesVersion, stdin io.ReadSeeker, args ...string) error
	// RunQuietlyAndIgnore runs a command quietly and ignores errors
	RunQuietlyAndIgnore(cmd constants.IptablesCmd, iptVer *IptablesVersion, stdin io.ReadSeeker, args ...string)

	// DetectIptablesVersion consults the available binaries and in-use tables to determine
	// which iptables variant (legacy, nft, v6, v4) we should use in the current context.
	DetectIptablesVersion(ipV6 bool) (IptablesVersion, error)
}
