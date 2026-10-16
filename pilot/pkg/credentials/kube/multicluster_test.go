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

package kube

import (
	"testing"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/multicluster"
	"istio.io/istio/pkg/test"
)

func TestForClusterWithRemoteCredentialsDisabled(t *testing.T) {
	test.SetForTest(t, &features.EnableRemoteCredentialsController, false)

	stop := test.NewStop(t)
	localClient := kube.NewFakeClient()
	localClient.RunAndWait(stop)
	remoteClient := kube.NewFakeClient()
	remoteClient.RunAndWait(stop)

	mc := multicluster.NewFakeController()
	sc := NewMulticluster("local", mc)
	mc.Add("local", localClient, stop)
	mc.Add("remote", remoteClient, stop)

	cases := []struct {
		cluster   cluster.ID
		expectErr bool
	}{
		// Config cluster always works
		{"local", false},
		// Remote cluster successfully returns the AggregateController (with a nil authController)
		{"remote", false},
		// Unknown cluster throws an error
		{"invalid", true},
	}

	for _, tt := range cases {
		t.Run(string(tt.cluster), func(t *testing.T) {
			_, err := sc.ForCluster(tt.cluster)
			if (err != nil) != tt.expectErr {
				t.Fatalf("expected err=%v, got err=%v", tt.expectErr, err)
			}
		})
	}
}

func TestAuthorizeWithRemoteCredentialsDisabled(t *testing.T) {
	test.SetForTest(t, &features.EnableRemoteCredentialsController, false)

	stop := test.NewStop(t)
	localClient := kube.NewFakeClient()
	localClient.RunAndWait(stop)
	allowIdentities(localClient, "system:serviceaccount:ns-local:sa-allowed")

	remoteClient := kube.NewFakeClient()
	remoteClient.RunAndWait(stop)

	mc := multicluster.NewFakeController()
	sc := NewMulticluster("local", mc)
	mc.Add("local", localClient, stop)
	mc.Add("remote", remoteClient, stop) // Add remote cluster to test auth rejection

	// 1. Config cluster should still allow authorization
	con, err := sc.ForCluster("local")
	if err != nil {
		t.Fatal(err)
	}
	if err := con.Authorize("sa-allowed", "ns-local"); err != nil {
		t.Fatalf("expected allowed, got err=%v", err)
	}

	// 2. Remote cluster should safely return the controller, but REJECT authorization
	remoteCon, err := sc.ForCluster("remote")
	if err != nil {
		t.Fatalf("expected no error getting remote cluster, got %v", err)
	}
	if err := remoteCon.Authorize("sa-allowed", "ns-local"); err != ErrNoAuthController {
		t.Fatalf("expected Authorize to fail with %v, actually got %v", ErrNoAuthController, err)
	}
}

func TestRemoteCredentialsDisabledSyncAndClose(t *testing.T) {
	test.SetForTest(t, &features.EnableRemoteCredentialsController, false)

	stop := test.NewStop(t)
	localClient := kube.NewFakeClient()
	localClient.RunAndWait(stop)
	remoteClient := kube.NewFakeClient()
	remoteClient.RunAndWait(stop)

	mc := multicluster.NewFakeController()
	sc := NewMulticluster("local", mc)
	mc.Add("local", localClient, stop)
	mc.Add("remote", remoteClient, stop)

	// The multicluster component calls HasSynced on every controller it holds, including the nil
	// one stored for the remote cluster. This must not panic and must not block the cluster sync.
	sawNil := false
	for _, c := range sc.component.All() {
		if c == nil {
			sawNil = true
			if !c.HasSynced() {
				t.Fatal("expected disabled controller to report synced")
			}
		}
	}
	if !sawNil {
		t.Fatal("expected a nil controller for the remote cluster")
	}

	// Removing the remote cluster calls Close on the nil controller. This must not panic.
	mc.Delete("remote")
	if _, err := sc.ForCluster("remote"); err == nil {
		t.Fatal("expected error for deleted cluster")
	}
}
