package controller

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/serviceregistry/util/xdsfake"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

const systemNS = "istio-system"

var cniPodLabels = map[string]string{
	"k8s-app":      "istio-cni-node",
	"istio.io/rev": "default",
}

type nodeTainterTestServer struct {
	controller *FakeController
	fx         *xdsfake.Updater
	pc         clienttest.TestClient[*corev1.Pod]
	nc         clienttest.TestClient[*corev1.Node]
	t          *testing.T
}

func newNodeUntainterTestServer(t *testing.T) *nodeTainterTestServer {
	controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{
		SystemNamespace: systemNS,
	})

	pc := clienttest.Wrap(t, controller.podsClient)
	nc := clienttest.Wrap(t, controller.nodes)

	return &nodeTainterTestServer{
		t:          t,
		controller: controller,
		fx:         fx,
		pc:         pc,
		nc:         nc,
	}
}

func TestNodeUntainter(t *testing.T) {
	test.SetForTest(t, &features.EnableNodeUntaintControllers, true)
	s := newNodeUntainterTestServer(t)
	s.addNodes(t, "node1", "node2", "node3")
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
	s.addNodes(t, "node1", "node2")
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

func (s *nodeTainterTestServer) addNodes(t *testing.T, nodes ...string) {
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
