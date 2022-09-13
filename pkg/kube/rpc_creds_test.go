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
	"context"
	"reflect"
	"testing"
	"time"

	v1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	"istio.io/istio/pkg/test/util/retry"
)

func Test_tokenSupplier_GetRequestMetadata(t *testing.T) {
	ctx := context.Background()
	cli := NewFakeClient()
	clientset := cli.Kube().(*fake.Clientset)
	clientset.PrependReactor("create", "serviceaccounts",
		func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
			act, ok := action.(clienttesting.CreateActionImpl)
			if !ok {
				return false, nil, nil
			}
			tokenReq, ok := act.Object.(*v1.TokenRequest)
			if !ok {
				return false, nil, nil
			}
			t := tokenReq.DeepCopy()
			now := time.Now()
			t.Status.ExpirationTimestamp = metav1.NewTime(now.Add(time.Duration(*t.Spec.ExpirationSeconds) * time.Second))
			t.Status.Token = rand.String(16)
			return true, t, nil
		},
	)

	const (
		refreshSeconds      = 1
		expirationSeconds   = 60
		sunsetPeriodSeconds = expirationSeconds - refreshSeconds
	)

	perCred, err := NewRPCCredentials(cli, "default", "default", nil, expirationSeconds, sunsetPeriodSeconds)
	if err != nil {
		t.Fatal(err)
	}
	m1, err := perCred.GetRequestMetadata(ctx)
	if err != nil {
		t.Fatal(err)
	}

	m2, err := perCred.GetRequestMetadata(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(m1, m2) {
		t.Fatalf("Unexpectedly getting a new tokens")
	}

	var m3 map[string]string
	retry.UntilOrFail(t,
		func() bool {
			m3, err = perCred.GetRequestMetadata(ctx)
			return err == nil && !reflect.DeepEqual(m1, m3)
		},
		retry.Delay(refreshSeconds*time.Second),
		retry.Timeout(expirationSeconds*time.Second),
	)
	if reflect.DeepEqual(m1, m3) {
		t.Fatalf("Unexpectedly not getting a new token")
	}
}
