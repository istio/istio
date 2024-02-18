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
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/cni/pkg/util"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/util/assert"
)

func TestProcessAddEventGoodPayload(t *testing.T) {
	valid := CNIPluginAddEvent{
		Netns:        "/var/netns/foo",
		PodName:      "pod-bingo",
		PodNamespace: "funkyns",
	}

	payload, _ := json.Marshal(valid)

	addEvent, err := processAddEvent(payload)

	assert.NoError(t, err)
	assert.Equal(t, valid, addEvent)
}

func TestProcessAddEventBadPayload(t *testing.T) {
	valid := CNIPluginAddEvent{
		Netns:        "/var/netns/foo",
		PodName:      "pod-bingo",
		PodNamespace: "funkyns",
	}

	payload, _ := json.Marshal(valid)

	invalid := string(payload) + "funkyjunk"

	_, err := processAddEvent([]byte(invalid))

	assert.Error(t, err)
}

func TestCNIPluginServer(t *testing.T) {
	fakePodIP := "11.1.1.12"
	_, addr, _ := net.ParseCIDR(fakePodIP + "/32")
	valid := CNIPluginAddEvent{
		Netns:        "/var/netns/foo",
		PodName:      "pod-bingo",
		PodNamespace: "funkyns",
		IPs: []IPConfig{{
			Address: *addr,
		}},
	}

	setupLogging()
	NodeName = "testnode"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-bingo",
			Namespace: "funkyns",
		},
		Spec: corev1.PodSpec{
			NodeName: NodeName,
		},
		Status: corev1.PodStatus{
			PodIP: fakePodIP,
		},
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "funkyns"}}

	client := kube.NewFakeClient(ns, pod)

	// We are expecting at most 1 calls to the mock, wait for them
	wg, waitForMockCalls := NewWaitForNCalls(t, 1)
	fs := &fakeServer{testWG: wg}

	fs.On("AddPodToMesh",
		ctx,
		pod,
		util.GetPodIPsIfPresent(pod),
		valid.Netns,
	).Return(nil)

	dpServer := &meshDataplane{
		kubeClient: client.Kube(),
		netServer:  fs,
	}

	handlers := setupHandlers(ctx, client, dpServer, "istio-system")

	// We are not going to start the server, so the sockpath is irrelevant
	pluginServer := startCniPluginServer(ctx, "/tmp/test.sock", handlers, dpServer)

	// label the namespace
	labelsPatch := []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"%s"}}}`,
		constants.DataplaneMode, constants.DataplaneModeAmbient))
	_, err := client.Kube().CoreV1().Namespaces().Patch(ctx, ns.Name,
		types.MergePatchType, labelsPatch, metav1.PatchOptions{})
	assert.NoError(t, err)

	client.RunAndWait(ctx.Done())

	payload, _ := json.Marshal(valid)

	// serialize our fake plugin event
	addEvent, err := processAddEvent(payload)
	assert.Equal(t, err, nil)

	// Push it thru the handler
	pluginServer.ReconcileCNIAddEvent(ctx, addEvent)

	waitForMockCalls()

	assertPodAnnotated(t, client, pod)
	// Assert expected calls actually made
	fs.AssertExpectations(t)
}

func TestCNIPluginServerPrefersCNIProvidedPodIP(t *testing.T) {
	fakePodIP := "11.1.1.12"
	_, addr, _ := net.ParseCIDR(fakePodIP + "/32")
	valid := CNIPluginAddEvent{
		Netns:        "/var/netns/foo",
		PodName:      "pod-bingo",
		PodNamespace: "funkyns",
		IPs: []IPConfig{{
			Address: *addr,
		}},
	}

	setupLogging()
	NodeName = "testnode"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-bingo",
			Namespace: "funkyns",
		},
		Spec: corev1.PodSpec{
			NodeName: NodeName,
		},
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "funkyns"}}

	client := kube.NewFakeClient(ns, pod)

	// We are expecting at most 1 calls to the mock, wait for them
	wg, waitForMockCalls := NewWaitForNCalls(t, 1)
	fs := &fakeServer{testWG: wg}

	// This pod should be enmeshed with the CNI ip, even tho the pod status had no ip
	fs.On("AddPodToMesh",
		ctx,
		pod,
		[]netip.Addr{netip.MustParseAddr(fakePodIP)},
		valid.Netns,
	).Return(nil)

	dpServer := &meshDataplane{
		kubeClient: client.Kube(),
		netServer:  fs,
	}

	handlers := setupHandlers(ctx, client, dpServer, "istio-system")

	// We are not going to start the server, so the sockpath is irrelevant
	pluginServer := startCniPluginServer(ctx, "/tmp/test.sock", handlers, dpServer)

	// label the namespace
	labelsPatch := []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"%s"}}}`,
		constants.DataplaneMode, constants.DataplaneModeAmbient))
	_, err := client.Kube().CoreV1().Namespaces().Patch(ctx, ns.Name,
		types.MergePatchType, labelsPatch, metav1.PatchOptions{})
	assert.NoError(t, err)

	client.RunAndWait(ctx.Done())

	payload, _ := json.Marshal(valid)

	// serialize our fake plugin event
	addEvent, err := processAddEvent(payload)
	assert.Equal(t, err, nil)

	// Push it thru the handler
	pluginServer.ReconcileCNIAddEvent(ctx, addEvent)

	waitForMockCalls()

	assertPodAnnotated(t, client, pod)
	// Assert expected calls actually made
	fs.AssertExpectations(t)
}
