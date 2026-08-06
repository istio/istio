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

package nodeagent

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// nft exits non-zero for many transient reasons (EPERM, module not loaded, etc.).
// Only this specific string identifies a build-time omission of libjansson,
// which is permanent and requires a backend switch. All other non-zero exits are
// treated as probe failures and handled with a warning, leaving nftables selected.
const nftJSONNotCompiledMarker = "JSON support not compiled-in"

// Overridable by tests.
var nftJSONProbeCommandFn = func(ctx context.Context) *exec.Cmd {
	return exec.CommandContext(ctx, "nft", "--json", "list", "tables")
}

// detectNftJSONSupport returns false if the nft binary lacks JSON output,
// in which case the ambient CNI agent should fall back to iptables.
func detectNftJSONSupport() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := nftJSONProbeCommandFn(ctx)
	out, err := cmd.CombinedOutput()
	outStr := string(out)
	if strings.Contains(outStr, nftJSONNotCompiledMarker) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("nft JSON probe failed (output: %q): %w", strings.TrimSpace(outStr), err)
	}
	return true, nil
}
