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

package ambient

import (
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/pkg/log"
)

var (
	_ resource.Resource = &redirection{}

	retryOpts  = []retry.Option{retry.Timeout(10 * time.Second), retry.Delay(time.Second)}
	redirectSh = path.Join(env.IstioSrc, "redirect.sh")
	ipsetSh    = path.Join(env.IstioSrc, "tmp-update-pod-set.sh")
)

type redirection struct {
	id resource.ID
}

func (r *redirection) ID() resource.ID {
	return r.id
}

// Redirection component for the test framework enables redirection to uProxy for the duration of the suite.
// TODO This is super hacky for now. Eventually, this should enable/disable the CNI.
func Redirection(ctx resource.Context) error {
	if !ctx.Settings().CIMode {
		return nil
	}
	if strings.HasPrefix("TEST_ENV", "gke") {
		scopes.Framework.Infof("=== SKIPPED: Apply ambient iptables redirection ===")
		return nil
	}
	scopes.Framework.Infof("=== BEGIN: Apply ambient iptables redirection ===")
	defer func() {
		scopes.Framework.Infof("=== DONE: Apply ambient iptables redirection ===")
	}()
	if !strings.HasPrefix(os.Getenv("TEST_ENV"), "kind") {
		scopes.Framework.Warn("Redirection only works for kind. Set TEST_ENV=gke to skip this.")
	}
	r := &redirection{}
	r.id = ctx.TrackResource(r)

	dir, err := ctx.CreateDirectory("ambient-redirection-logs")
	if err != nil {
		scopes.Framework.Error(err)
	}

	errG := multierror.Group{}
	// TODO this is written for multi-cluster, but these scripts won't support multi-cluster
	for _, c := range ctx.Clusters() {
		cName := c.Name()
		if len(ctx.Clusters()) == 1 {
			// this is the kind name; we'll want a way to match it with the tf cluster-name as long as we need that name
			cName = "istio-testing"
			if override := os.Getenv("KIND_NAME"); override != "" {
				cName = override
			}
		}
		errG.Go(func() error {
			if err := enableRedirection(ctx, dir, cName); err != nil {
				return err
			}
			if err := updateIPSet(ctx, dir, cName); err != nil {
				return err
			}
			return nil
		})
	}
	return errG.Wait().ErrorOrNil()
}

func enableRedirection(ctx resource.Context, dir string, clusterName string) error {
	f, err := os.Create(path.Join(dir, "redirect.sh.log"))
	if err != nil {
		scopes.Framework.Error(err)
	}

	err = retry.UntilSuccess(func() error {
		redirCmd := exec.Command(redirectSh, clusterName)
		if f != nil {
			log.Infof("redirect.sh output to %s", f.Name())
			_, _ = f.WriteString("---\n")
			redirCmd.Stdout = f
		}
		scopes.Framework.Infof("Running %q", redirCmd.String())
		return redirCmd.Run()
	}, retryOpts...)
	if err != nil {
		return err
	}
	ctx.CleanupConditionally(func() {
		if f != nil {
			_ = f.Close()
		}
		if err := retry.UntilSuccess(func() error {
			cleanCmd := exec.Command(redirectSh, clusterName, "clean")
			scopes.Framework.Infof("Running %q", cleanCmd.String())
			return cleanCmd.Run()
		}, retryOpts...); err != nil {
			scopes.Framework.Warnf("failed cleaning up redirection for %s: %v", clusterName, err)
		}
	})
	return nil
}

func updateIPSet(ctx resource.Context, dir string, clusterName string) error {
	var ipsetCmd *exec.Cmd
	f, err := os.Create(path.Join(dir, "ipset.log"))
	if err != nil {
		scopes.Framework.Error(err)
	}

	if err := retry.UntilSuccess(func() error {
		ipsetCmd = exec.Command(ipsetSh, clusterName)
		if f != nil {
			log.Infof("ipset command output to %s", f.Name())

			_, _ = f.WriteString("---\n")
			ipsetCmd.Stdout = f
		}
		scopes.Framework.Infof("Starting %q", ipsetCmd.String())
		return ipsetCmd.Start()
	}, retryOpts...); err != nil {
		return err
	}
	ctx.Cleanup(func() {
		if ipsetCmd.Process == nil {
			return
		}
		scopes.Framework.Infof("Killing pid %d for %s", ipsetCmd.Process.Pid, clusterName)
		_ = ipsetCmd.Process.Kill()
		if f != nil {
			_ = f.Close()
		}
	})
	return nil
}
