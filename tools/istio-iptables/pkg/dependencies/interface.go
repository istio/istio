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
	"bytes"
	"io"

	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
)

// Dependencies is used as abstraction for the commands used from the operating system
type Dependencies interface {
	// Run runs a command
	Run(log *istiolog.Scope, quietLogging bool, cmd constants.IptablesCmd, iptVer *IptablesVersion, stdin io.ReadSeeker, args ...string) (*bytes.Buffer, error)

	// DetectIptablesVersion consults the available binaries and in-use tables to determine
	// which iptables variant (legacy, nft, v6, v4) we should use in the current context.
	// NOTE that this uses existing rules as part of its heuristic when choosing which binary
	// to use, so detection should typically happen *once-per-netns*, or different results
	// might be returned on subsequent calls if the rules in the netnamespace have changed.
	DetectIptablesVersion(ipV6, forceLegacy bool) (IptablesVersion, error)
}
