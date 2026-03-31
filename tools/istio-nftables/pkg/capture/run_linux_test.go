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

package capture

import (
	"bytes"
	"os/exec"
	"sync"
	"testing"

	// Create a new network namespace. This will have the 'lo' interface ready but nothing else.
	_ "github.com/howardjohn/unshare-go/netns"
	// Create a new user namespace. This will map the current UID/GID to 0.
	"github.com/howardjohn/unshare-go/userns"
	"sigs.k8s.io/knftables"

	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/tools/istio-nftables/pkg/builder"
)

func dumpRuleset(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("nft", "list", "ruleset")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Fatalf("Failed to dump nftables ruleset: %v\nStderr: %s", err, stderr.String())
	}
	return stdout.String()
}

func TestIdempotentEquivalentRerun(t *testing.T) {
	setup(t)
	commonCases := getCommonTestCases()
	for _, tt := range commonCases {
		t.Run(tt.name, func(t *testing.T) {
			cfg := constructTestConfig()
			tt.config(cfg)
			if cfg.OwnerGroupsExclude != "" {
				cfg.OwnerGroupsInclude = "0"
			}
			if cfg.OwnerGroupsInclude != "" {
				cfg.OwnerGroupsInclude = "0"
			}
			// This provider function will interact with the real system's nftables.
			nftProvider := func(_ knftables.Family, _ string) (builder.NftablesAPI, error) {
				return builder.NewNftImpl("", "")
			}

			// Cleanup logic
			defer func() {
				cfg.CleanupOnly = true
				cleanupConfigurator, err := NewNftablesConfigurator(cfg, nftProvider)
				assert.NoError(t, err)
				_, err = cleanupConfigurator.Run()
				assert.NoError(t, err)
				assert.Equal(t, dumpRuleset(t), "", "nftables should be clean after cleanup")
			}()

			// First pass
			firstPassConfigurator, err := NewNftablesConfigurator(cfg, nftProvider)
			assert.NoError(t, err)
			_, err = firstPassConfigurator.Run()
			assert.NoError(t, err)
			firstPassDump := dumpRuleset(t)
			if firstPassDump == "" {
				t.Fatalf("First pass configuration resulted in an empty nftables ruleset dump")
			}
			// Second Pass
			secondPassConfigurator, err := NewNftablesConfigurator(cfg, nftProvider)
			assert.NoError(t, err)
			_, err = secondPassConfigurator.Run()
			assert.NoError(t, err)
			secondPassDump := dumpRuleset(t)
			if secondPassDump == "" {
				t.Fatalf("Second pass configuration resulted in an empty nftables ruleset dump")
			}
			// The ruleset should be identical, despite the rerun
			assert.Equal(t, firstPassDump, secondPassDump, "nftables ruleset should be identical after a second run")
		})
	}
}

var initialized = &sync.Once{}

func setup(t *testing.T) {
	initialized.Do(func() {
		// Setup group namespace so nftables skgid conditions will work
		assert.NoError(t, userns.WriteGroupMap(map[uint32]uint32{userns.OriginalGID(): 0}))
		// Istio nftables expects to find a non-localhost IP in some interface
		assert.NoError(t, exec.Command("ip", "addr", "add", "240.240.240.240/32", "dev", "lo").Run())
	})
}
