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

package untaint

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pilot/pkg/features"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

const systemNS = "istio-system"

var cniPodLabels = map[string]string{
	"k8s-app":    "istio-cni-node",
	"some-other": "label",
}

type nodeTainterTestServer struct {
	client kubelib.Client
	pc     clienttest.TestClient[*corev1.Pod]
	nc     clienttest.TestClient[*corev1.Node]
	t      *testing.T
}

func setupLogging() {
	opts := istiolog.DefaultOptions()
	opts.SetDefaultOutputLevel(istiolog.OverrideScopeName, istiolog.DebugLevel)
	istiolog.Configure(opts)
	for _, scope := range istiolog.Scopes() {
		scope.SetOutputLevel(istiolog.DebugLevel)
	}
}

func newNodeUntainterTestServer(t *testing.T) *nodeTainterTestServer {
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	client := kubelib.NewFakeClient()

	nodeUntainter := NewNodeUntainter(stop, client, systemNS, systemNS, krt.GlobalDebugHandler)
	go nodeUntainter.Run(stop)
	client.RunAndWait(stop)
	kubelib.WaitForCacheSync("test", stop, nodeUntainter.HasSynced)

	pc := clienttest.Wrap(t, nodeUntainter.podsClient)
	nc := clienttest.Wrap(t, nodeUntainter.nodesClient)

	return &nodeTainterTestServer{
		client: client,
		t:      t,
		pc:     pc,
		nc:     nc,
	}
}

func TestNodeUntainter(t *testing.T) {
	setupLogging()
	test.SetForTest(t, &features.EnableNodeUntaintControllers, true)
	s := newNodeUntainterTestServer(t)
	s.addTaintedNodes(t, "node1", "node2", "node3")
	s.addPod(t, "node3", true, map[string]string{"k8s-app": "other-app"}, "")
	s.addCniPod(t, "node2", false)
	s.addCniPod(t, "node1", true)
	s.assertNodeUntainted(t, "node1")
	s.assertNodeTainted(t, "node2")
	s.assertNodeTainted(t, "node3")
}

func TestNodeUntainterOnlyUntaintsWhenIstiocniInourNs(t *testing.T) {
	test.SetForTest(t, &features.EnableNodeUntaintControllers, true)
	s := newNodeUntainterTestServer(t)
	s.addTaintedNodes(t, "node1", "node2")
	s.addPod(t, "node2", true, cniPodLabels, "default")
	s.addCniPod(t, "node1", true)

	// wait for the untainter to run
	s.assertNodeUntainted(t, "node1")
	s.assertNodeTainted(t, "node2")
}

func (s *nodeTainterTestServer) assertNodeTainted(t *testing.T, node string) {
	t.Helper()
	assert.Equal(t, s.isNodeUntainted(node), false)
}

func (s *nodeTainterTestServer) assertNodeUntainted(t *testing.T, node string) {
	t.Helper()
	assert.EventuallyEqual(t, func() bool {
		return s.isNodeUntainted(node)
	}, true, retry.Timeout(time.Second*3))
}

func (s *nodeTainterTestServer) isNodeUntainted(node string) bool {
	n := s.nc.Get(node, "")
	if n == nil {
		return false
	}
	for _, t := range n.Spec.Taints {
		if t.Key == TaintName {
			return false
		}
	}
	return true
}

func (s *nodeTainterTestServer) addTaintedNodes(t *testing.T, nodes ...string) {
	t.Helper()
	for _, node := range nodes {
		node := generateNode(node, nil)
		// add our special taint
		node.Spec.Taints = append(node.Spec.Taints, corev1.Taint{
			Key:    TaintName,
			Value:  "true",
			Effect: corev1.TaintEffectNoSchedule,
		})
		s.nc.Create(node)
	}
}

func (s *nodeTainterTestServer) addCniPod(t *testing.T, node string, markReady bool) {
	s.addPod(t, node, markReady, cniPodLabels, systemNS)
}

func (s *nodeTainterTestServer) addPod(t *testing.T, node string, markReady bool, labels map[string]string, ns string) {
	t.Helper()
	ip := "1.2.3.4"
	name := "istio-cni-" + node
	if ns == "" {
		ns = systemNS
	}
	pod := generatePod(ip, name, ns, "sa", node, labels, nil)

	p := s.pc.Get(name, pod.Namespace)
	if p == nil {
		// Apiserver doesn't allow Create to modify the pod status; in real world it's a 2 part process
		pod.Status = corev1.PodStatus{}
		newPod := s.pc.Create(pod)
		if markReady {
			setPodReady(newPod)
		}
		newPod.Status.PodIP = ip
		newPod.Status.Phase = corev1.PodRunning
		newPod.Status.PodIPs = []corev1.PodIP{
			{
				IP: ip,
			},
		}
		s.pc.UpdateStatus(newPod)
	} else {
		s.pc.Update(pod)
	}
}

func generateNode(name string, labels map[string]string) *corev1.Node {
	return &corev1.Node{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Node",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

func generatePod(ip, name, namespace, saName, node string, labels map[string]string, annotations map[string]string) *corev1.Pod {
	automount := false
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Labels:      labels,
			Annotations: annotations,
			Namespace:   namespace,
		},
		Spec: corev1.PodSpec{
			ServiceAccountName:           saName,
			NodeName:                     node,
			AutomountServiceAccountToken: &automount,
			// Validation requires this
			Containers: []corev1.Container{
				{
					Name:  "test",
					Image: "ununtu",
				},
			},
		},
		// The cache controller uses this as key, required by our impl.
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:               corev1.PodReady,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
				},
			},
			PodIP:  ip,
			HostIP: ip,
			PodIPs: []corev1.PodIP{
				{
					IP: ip,
				},
			},
			Phase: corev1.PodRunning,
		},
	}
}

func setPodReady(pod *corev1.Pod) {
	pod.Status.Conditions = []corev1.PodCondition{
		{
			Type:               corev1.PodReady,
			Status:             corev1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
		},
	}
}
