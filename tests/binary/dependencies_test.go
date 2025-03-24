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

package binary

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

// TestDependencies controls which binaries can import which packages.
// Note we also use linting (depguard), but that only handles direct dependencies, while this covers indirect ones.
func TestDependencies(t *testing.T) {
	tests := []struct {
		entrypoint string
		tag        string
		// Regex of dependencies we do not allow
		denied []string
		// Exceptions to the above. This allows a denial like `foo/` with an exception for `foo/bar/bz`
		exceptions []string
		// Things we wish were not import
		wantToDeny []string
	}{
		{
			entrypoint: "pilot/cmd/pilot-agent",
			tag:        "agent,disable_pgv",
			exceptions: []string{
				`^k8s\.io/(utils|klog)/`,
				`^k8s\.io/apimachinery/pkg/types`,
				`^k8s\.io/apimachinery/pkg/version`,
				`^k8s\.io/apimachinery/pkg/util/(rand|version)`,
				`envoy/type/|envoy/annotations|envoy/config/core/`,
				`envoy/extensions/transport_sockets/tls/`,
				`envoy/service/(discovery|secret)/v3`,
				`envoy/extensions/wasm/`,
				`envoy/extensions/filters/(http|network)/wasm/`,
				`github.com/envoyproxy/protoc-gen-validate/validate`,
				`github.com/envoyproxy/go-control-plane/envoy/config`,
				`github.com/envoyproxy/go-control-plane/envoy/extensions/filters`,
				`contrib/envoy/extensions/private_key_providers/`,
				`^istio\.io/api/(annotation|label|mcp|mesh|networking|type)`,
				`^istio\.io/api/analysis/v1alpha1`,
				`^istio\.io/api/meta/v1alpha1`,
				`^istio\.io/api/security/v1alpha1`,
			},
			denied: []string{
				`^k8s\.io`,
				`^sigs\.k8s\.io/gateway-api`,
				`^github\.com/google/cel-go`,
				`^github\.com/lestrrat-go/jwx/jwk`,
				`^github\.com/envoyproxy`,
				`^istio\.io/api`,
				`^sigs\.k8s\.io/controller-runtime`,
			},
			wantToDeny: []string{
				`^testing$`,
			},
		},
		{
			entrypoint: "pilot/cmd/pilot-discovery",
			tag:        "vtprotobuf,disable_pgv",
			exceptions: []string{
				"sigs.k8s.io/controller-runtime/pkg/scheme",
			},
			denied: []string{
				// Deps meant only for other components; if we import them, something may be wrong
				`^github\.com/containernetworking/`,
				`^github\.com/fatih/color`,
				`^github\.com/vishvananda/`,
				`^helm\.sh/helm/v3`,
				`^sigs\.k8s\.io/controller-runtime`,
				// Testing deps
				`^github\.com/AdaLogics/go-fuzz-headers`,
				`^github\.com/google/shlex`,
				`^github\.com/howardjohn/unshare-go`,
				`^github\.com/pmezard/go-difflib`,
			},
			wantToDeny: []string{
				`^testing$`,
			},
		},
		{
			entrypoint: "istioctl/cmd/istioctl",
			exceptions: []string{
				"sigs.k8s.io/controller-runtime/pkg/scheme",
			},
			denied: []string{
				// Deps meant only for other components; if we import them, something may be wrong
				`^github\.com/containernetworking/`,
				`^github\.com/vishvananda/`,
				`^sigs\.k8s\.io/controller-runtime`,
				// Testing deps
				`^github\.com/AdaLogics/go-fuzz-headers`,
				`^github\.com/howardjohn/unshare-go`,
			},
			wantToDeny: []string{
				`^testing$`,
			},
		},
	}
	allDenials := []*regexp.Regexp{}
	for _, tt := range tests {
		t.Run(tt.entrypoint, func(t *testing.T) {
			deps, err := getDependencies(filepath.Join(env.IstioSrc, tt.entrypoint), tt.tag, false)
			assert.NoError(t, err)
			denies, err := slices.MapErr(tt.denied, regexp.Compile)
			allDenials = append(allDenials, denies...)
			assert.NoError(t, err)
			exceptions, err := slices.MapErr(tt.exceptions, regexp.Compile)
			assert.NoError(t, err)
			maybeCanMoveToDeny := map[string]*regexp.Regexp{}
			for _, wd := range tt.wantToDeny {
				maybeCanMoveToDeny[wd] = regexp.MustCompile(wd)
			}
			assert.NoError(t, err)
			unseenExceptions := sets.New[string](tt.exceptions...)
			for _, dep := range deps {
				for _, deny := range denies {
					if !deny.MatchString(dep) {
						continue
					}
					allowed := false
					for _, allow := range exceptions {
						if allow.MatchString(dep) {
							unseenExceptions.Delete(allow.String())
							allowed = true
							break
						}
					}
					if !allowed {
						t.Errorf("illegal dependency: %v", dep)
					}
				}
				for _, wantDeny := range maybeCanMoveToDeny {
					if wantDeny.MatchString(dep) {
						// We want to deny it, but we are depending on it.. cannot deny it
						delete(maybeCanMoveToDeny, wantDeny.String())
					}
				}
			}
			for us := range unseenExceptions {
				t.Errorf("exception %q was never matched, maybe redundant?", us)
			}
			for rgx := range maybeCanMoveToDeny {
				t.Errorf("we wanted to deny %q, and it was not found! move it to the denials list", rgx)
			}
			t.Logf("%d total dependencies", len(deps))
		})
	}
	t.Run("exhaustive", func(t *testing.T) {
		all, err := getDependencies(env.IstioSrc+"/...", "integ", true)
		assert.NoError(t, err)
		for _, d := range allDenials {
			found := false
			for _, dep := range all {
				if d.MatchString(dep) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Had a deny rule %q, but it doesn't match *any* dependency in the repo. This is likely a bug.", d)
			}
		}
		t.Logf("%d total dependencies", len(all))
	})
}

func getDependencies(path, tag string, tests bool) ([]string, error) {
	args := []string{"list", "-mod=readonly", "-f", `{{ join .Deps "\n" }}`, "-tags=" + tag}
	if tests {
		args = append(args, "-test")
	}
	args = append(args, path)
	cmd := exec.Command("go", args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("%v: %v", err, stderr.String())
	}
	modules := strings.Split(stdout.String(), "\n")
	modules = slices.Sort(modules)
	modules = slices.FilterDuplicatesPresorted(modules)

	return modules, nil
}
