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

package gateway

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/atomic"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	k8s "sigs.k8s.io/gateway-api/apis/v1"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pilot/pkg/status"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/clustertrustbundle"
	"istio.io/istio/pkg/kube/clustertrustbundle/clustertrustbundletest"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	ctbEchoHost = "echo.default.svc.domain.suffix"
	ctbName     = "my-bundle"
)

func newTestBundle(pemData string) *certificatesv1.ClusterTrustBundle {
	return &certificatesv1.ClusterTrustBundle{
		ObjectMeta: metav1.ObjectMeta{Name: ctbName},
		Spec:       certificatesv1.ClusterTrustBundleSpec{TrustBundle: pemData},
	}
}

// ctbStatusQueue also remembers every status written for policies and Gateways, so tests can assert that a wrong status
// was never reported, not only that the final one is right.
type ctbStatusQueue struct {
	*TestStatusQueue
	histMu  sync.Mutex
	history []string
}

func (q *ctbStatusQueue) EnqueueStatusUpdateResource(context any, target status.Resource) {
	var conds []metav1.Condition
	switch st := context.(type) {
	case *k8s.PolicyStatus:
		if len(st.Ancestors) > 0 {
			conds = st.Ancestors[0].Conditions
		}
	case *k8s.GatewayStatus:
		if len(st.Listeners) > 0 {
			conds = st.Listeners[0].Conditions
		}
	}
	if len(conds) > 0 {
		q.histMu.Lock()
		q.history = append(q.history, fmt.Sprintf("%s/%s: %s", target.Resource, target.Name, summarize(conds)))
		q.histMu.Unlock()
	}
	q.TestStatusQueue.EnqueueStatusUpdateResource(context, target)
}

func (q *ctbStatusQueue) seen(substr string) bool {
	q.histMu.Lock()
	defer q.histMu.Unlock()
	for _, h := range q.history {
		if strings.Contains(h, substr) {
			return true
		}
	}
	return false
}

type ctbEnv struct {
	t       *testing.T
	kc      kube.CLIClient
	api     *clustertrustbundletest.APIToggle
	ctrl    *Controller
	sq      *ctbStatusQueue
	bundles clienttest.TestWriter[*certificatesv1.ClusterTrustBundle]
	// informerBlocked is set when the ClusterTrustBundle informer can never sync, so RunAndWait would not return.
	informerBlocked bool
}

// When apiServed is false, e.api.Enable makes the API appear on a later discovery poll.
func newCTBEnv(t *testing.T, apiServed bool, objs ...ctbOption) *ctbEnv {
	test.SetForTest(t, &features.EnableGatewayAPIGatewayClassController, false)
	test.SetForTest(t, &clustertrustbundle.PollInterval, 10*time.Millisecond)
	stop := test.NewStop(t)
	kc := kube.NewFakeClient(
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "echo", Namespace: "default"}, Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{Name: "http", Port: 80}},
		}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "istio-system"}},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "my-cert", Namespace: "istio-system"},
			Data:       map[string][]byte{"tls.crt": []byte(rsaCertPEM), "tls.key": []byte(rsaKeyPEM)},
		},
	)
	setupClientCRDs(t, kc)
	api := clustertrustbundletest.NewAPIToggle(kc)
	if apiServed {
		api.Enable()
	}
	e := &ctbEnv{
		t:       t,
		kc:      kc,
		api:     api,
		sq:      &ctbStatusQueue{TestStatusQueue: &TestStatusQueue{state: map[status.Resource]any{}}},
		bundles: clienttest.NewWriter[*certificatesv1.ClusterTrustBundle](t, kc),
	}
	for _, o := range objs {
		o(e)
	}
	e.ctrl = NewController(kc, AlwaysReady, controller.Options{DomainSuffix: "domain.suffix", KrtDebugger: &krt.DebugHandler{}}, nil)
	statusSyncers := e.ctrl.status.SetQueue(e.sq)
	e.ctrl.Reconcile(core.NewConfigGenTest(t, core.TestOptions{}).PushContext())
	go e.ctrl.Run(stop)
	if e.informerBlocked {
		go kc.RunAndWait(stop)
	} else {
		kc.RunAndWait(stop)
	}
	kube.WaitForCacheSync("test", stop, e.ctrl.HasSynced)
	for _, st := range statusSyncers {
		st.WaitUntilSynced(stop)
	}
	return e
}

type ctbOption func(e *ctbEnv)

func withBundle(b *certificatesv1.ClusterTrustBundle) ctbOption {
	return func(e *ctbEnv) { e.bundles.Create(b) }
}

// withFailingDiscovery makes the first n discovery calls fail with a transient error.
func withFailingDiscovery(n int32) ctbOption {
	return func(e *ctbEnv) {
		left := atomic.NewInt32(n)
		clustertrustbundletest.InjectDiscoveryErrors(e.kc, func() error {
			if left.Dec() >= 0 {
				return errors.NewTimeoutError("discovery timed out", 1)
			}
			return nil
		})
	}
}

// withBundleListForbidden makes listing ClusterTrustBundles fail with Forbidden while forbidden is true.
func withBundleListForbidden(forbidden *atomic.Bool) ctbOption {
	return func(e *ctbEnv) {
		e.informerBlocked = true
		e.kc.Kube().(*fake.Clientset).PrependReactor("list", "clustertrustbundles", func(k8stesting.Action) (bool, runtime.Object, error) {
			if forbidden.Load() {
				return true, nil, errors.NewForbidden(clustertrustbundle.GVR.GroupResource(), "", fmt.Errorf("rbac"))
			}
			return false, nil, nil
		})
	}
}

func (e *ctbEnv) createBackendTLSPolicy() {
	clienttest.NewWriter[*k8s.BackendTLSPolicy](e.t, e.kc).Create(&k8s.BackendTLSPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "policy", Namespace: "default"},
		Spec: k8s.BackendTLSPolicySpec{
			TargetRefs: []k8s.LocalPolicyTargetReferenceWithSectionName{{
				LocalPolicyTargetReference: k8s.LocalPolicyTargetReference{Kind: "Service", Name: "echo"},
			}},
			Validation: k8s.BackendTLSPolicyValidation{
				CACertificateRefs: []k8s.LocalObjectReference{{
					Group: k8s.Group(clustertrustbundle.GVK.Group), Kind: k8s.Kind(clustertrustbundle.GVK.Kind), Name: ctbName,
				}},
				Hostname: "auth.example.com",
			},
		},
	})
}

func (e *ctbEnv) createGateway() {
	clienttest.NewWriter[*k8s.GatewayClass](e.t, e.kc).Create(&k8s.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{Name: "istio"},
		Spec:       k8s.GatewayClassSpec{ControllerName: "istio.io/gateway-controller"},
	})
	clienttest.NewWriter[*k8s.Gateway](e.t, e.kc).Create(&k8s.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw", Namespace: "istio-system"},
		Spec: k8s.GatewaySpec{
			GatewayClassName: "istio",
			TLS: &k8s.GatewayTLSConfig{Frontend: &k8s.FrontendTLSConfig{Default: k8s.TLSConfig{
				Validation: &k8s.FrontendTLSValidation{CACertificateRefs: []k8s.ObjectReference{{
					Group: k8s.Group(clustertrustbundle.GVK.Group), Kind: k8s.Kind(clustertrustbundle.GVK.Kind), Name: ctbName,
				}}},
			}}},
			Listeners: []k8s.Listener{{
				Name:     "https",
				Hostname: ptr.Of(k8s.Hostname("example.com")),
				Protocol: k8s.HTTPSProtocolType,
				Port:     443,
				TLS: &k8s.ListenerTLSConfig{
					Mode:            ptr.Of(k8s.TLSModeTerminate),
					CertificateRefs: []k8s.SecretObjectReference{{Name: "my-cert"}},
				},
			}},
		},
	})
}

// destinationRuleCredential returns the credentialName of the echo DestinationRule, or "<none>".
func (e *ctbEnv) destinationRuleCredential() string {
	for _, c := range e.ctrl.List(gvk.DestinationRule, "") {
		dr := c.Spec.(*networking.DestinationRule)
		if dr.Host == ctbEchoHost {
			return dr.GetTrafficPolicy().GetTls().GetCredentialName()
		}
	}
	return "<none>"
}

// gatewayCredential returns the caCertCredentialName of the emitted Gateway server, or "<none>" if the Gateway is not emitted.
func (e *ctbEnv) gatewayCredential() string {
	for _, c := range e.ctrl.List(gvk.Gateway, "") {
		gw := c.Spec.(*networking.Gateway)
		if len(gw.Servers) > 0 {
			return gw.Servers[0].GetTls().GetCaCertCredentialName()
		}
	}
	return "<none>"
}

func (e *ctbEnv) statusOf(res string, namespace, name string, out any) bool {
	e.sq.mu.Lock()
	defer e.sq.mu.Unlock()
	for k, v := range e.sq.state {
		if k.Resource == res && k.Namespace == namespace && k.Name == name {
			b, err := json.Marshal(v)
			assert.NoError(e.t, err)
			assert.NoError(e.t, json.Unmarshal(b, out))
			return true
		}
	}
	return false
}

// policyConditions summarizes the BackendTLSPolicy conditions as "Type=Status/Reason", sorted.
func (e *ctbEnv) policyConditions() string {
	return e.policyConditionsOf("policy")()
}

func (e *ctbEnv) policyConditionsOf(name string) func() string {
	return func() string {
		var st k8s.PolicyStatus
		if !e.statusOf(gvr.BackendTLSPolicy.Resource, "default", name, &st) || len(st.Ancestors) == 0 {
			return "<none>"
		}
		return summarize(st.Ancestors[0].Conditions)
	}
}

// listenerConditions summarizes the first listener's conditions of the Gateway.
func (e *ctbEnv) listenerConditions() string {
	var st k8s.GatewayStatus
	if !e.statusOf(gvr.KubernetesGateway.Resource, "istio-system", "gw", &st) || len(st.Listeners) == 0 {
		return "<none>"
	}
	var relevant []metav1.Condition
	for _, c := range st.Listeners[0].Conditions {
		if c.Type == string(k8s.ListenerConditionResolvedRefs) || c.Type == string(k8s.ListenerConditionAccepted) {
			relevant = append(relevant, c)
		}
	}
	return summarize(relevant)
}

func (e *ctbEnv) policyMessage(condType string) string {
	var st k8s.PolicyStatus
	e.statusOf(gvr.BackendTLSPolicy.Resource, "default", "policy", &st)
	if len(st.Ancestors) == 0 {
		return ""
	}
	for _, c := range st.Ancestors[0].Conditions {
		if c.Type == condType {
			return c.Message
		}
	}
	return ""
}

func summarize(conds []metav1.Condition) string {
	out := make([]string, 0, len(conds))
	for _, c := range conds {
		out = append(out, fmt.Sprintf("%s=%s/%s", c.Type, c.Status, c.Reason))
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

const (
	policyResolved        = "Accepted=True/Accepted,ResolvedRefs=True/ResolvedRefs"
	policyInvalidKind     = "Accepted=False/NoValidCACertificate,ResolvedRefs=False/InvalidKind"
	policyInvalidRef      = "Accepted=False/NoValidCACertificate,ResolvedRefs=False/InvalidCACertificateRef"
	listenerResolved      = "Accepted=True/Accepted,ResolvedRefs=True/ResolvedRefs"
	listenerInvalidKind   = "Accepted=False/NoValidCACertificate,ResolvedRefs=False/InvalidCACertificateKind"
	listenerInvalidRef    = "Accepted=False/NoValidCACertificate,ResolvedRefs=False/InvalidCACertificateRef"
	resolvedCredential    = "clustertrustbundle://" + ctbName
	invalidCredential     = "invalid://"
	noCredential          = "<none>"
	flagDisabledMsgSubstr = "disabled by PILOT_ENABLE_GATEWAY_API_CLUSTER_TRUST_BUNDLE"
	notServedMsgSubstr    = "certificates.k8s.io/v1 ClusterTrustBundle API is not served by the cluster"
)

func TestClusterTrustBundleDisabledByFlag(t *testing.T) {
	test.SetForTest(t, &features.EnableGatewayAPIClusterTrustBundle, false)
	// The API is served, and the bundle exists, but the feature flag wins.
	e := newCTBEnv(t, true, withBundle(newTestBundle(clustertrustbundletest.GenerateCA(t, "ca"))))
	e.createBackendTLSPolicy()
	e.createGateway()

	assert.EventuallyEqual(t, e.policyConditions, policyInvalidKind)
	assert.EventuallyEqual(t, e.destinationRuleCredential, invalidCredential)
	if msg := e.policyMessage("ResolvedRefs"); !strings.Contains(msg, flagDisabledMsgSubstr) {
		t.Fatalf("message %q does not mention the feature flag", msg)
	}
	assert.EventuallyEqual(t, e.listenerConditions, listenerInvalidKind)
	assert.Equal(t, e.gatewayCredential(), noCredential)
}

func TestClusterTrustBundleAPIBecomesAvailable(t *testing.T) {
	t.Run("bundle exists", func(t *testing.T) {
		e := newCTBEnv(t, false, withBundle(newTestBundle(clustertrustbundletest.GenerateCA(t, "ca"))))
		e.createBackendTLSPolicy()
		e.createGateway()

		assert.EventuallyEqual(t, e.policyConditions, policyInvalidKind)
		assert.EventuallyEqual(t, e.destinationRuleCredential, invalidCredential)
		if msg := e.policyMessage("ResolvedRefs"); !strings.Contains(msg, notServedMsgSubstr) {
			t.Fatalf("message %q does not mention the API not being served", msg)
		}
		assert.EventuallyEqual(t, e.listenerConditions, listenerInvalidKind)
		assert.Equal(t, e.gatewayCredential(), noCredential)

		e.api.Enable()
		assert.EventuallyEqual(t, e.policyConditions, policyResolved)
		assert.EventuallyEqual(t, e.destinationRuleCredential, resolvedCredential)
		assert.EventuallyEqual(t, e.listenerConditions, listenerResolved)
		assert.EventuallyEqual(t, e.gatewayCredential, resolvedCredential)
	})
	t.Run("bundle missing", func(t *testing.T) {
		e := newCTBEnv(t, false)
		e.createBackendTLSPolicy()
		e.createGateway()

		assert.EventuallyEqual(t, e.policyConditions, policyInvalidKind)
		assert.EventuallyEqual(t, e.listenerConditions, listenerInvalidKind)

		// A status computed while the API was absent must still flip even though no bundle ever showed up.
		e.api.Enable()
		assert.EventuallyEqual(t, e.policyConditions, policyInvalidRef)
		assert.EventuallyEqual(t, e.destinationRuleCredential, invalidCredential)
		assert.EventuallyEqual(t, e.listenerConditions, listenerInvalidRef)
		assert.Equal(t, e.gatewayCredential(), noCredential)

		e.bundles.Create(newTestBundle(clustertrustbundletest.GenerateCA(t, "ca")))
		assert.EventuallyEqual(t, e.policyConditions, policyResolved)
		assert.EventuallyEqual(t, e.destinationRuleCredential, resolvedCredential)
		assert.EventuallyEqual(t, e.listenerConditions, listenerResolved)
		assert.EventuallyEqual(t, e.gatewayCredential, resolvedCredential)
	})
}

func TestClusterTrustBundleBackendTLSPolicyLifecycle(t *testing.T) {
	ca1, ca2 := clustertrustbundletest.GenerateCA(t, "ca1"), clustertrustbundletest.GenerateCA(t, "ca2")
	e := newCTBEnv(t, true, withBundle(newTestBundle(ca1)))
	e.createBackendTLSPolicy()

	assert.EventuallyEqual(t, e.policyConditions, policyResolved)
	assert.EventuallyEqual(t, e.destinationRuleCredential, resolvedCredential)

	// Becoming unparsable must fail closed.
	e.bundles.Update(newTestBundle("not a certificate"))
	assert.EventuallyEqual(t, e.policyConditions, policyInvalidRef)
	assert.EventuallyEqual(t, e.destinationRuleCredential, invalidCredential)
	e.bundles.Update(newTestBundle(ca1))
	assert.EventuallyEqual(t, e.policyConditions, policyResolved)
	assert.EventuallyEqual(t, e.destinationRuleCredential, resolvedCredential)

	e.bundles.Delete(ctbName, "")
	assert.EventuallyEqual(t, e.policyConditions, policyInvalidRef)
	assert.EventuallyEqual(t, e.destinationRuleCredential, invalidCredential)

	e.bundles.Create(newTestBundle(ca2))
	assert.EventuallyEqual(t, e.policyConditions, policyResolved)
	assert.EventuallyEqual(t, e.destinationRuleCredential, resolvedCredential)
}

func TestClusterTrustBundleFrontendLifecycle(t *testing.T) {
	ca1, ca2 := clustertrustbundletest.GenerateCA(t, "ca1"), clustertrustbundletest.GenerateCA(t, "ca2")
	e := newCTBEnv(t, true, withBundle(newTestBundle(ca1)))
	e.createGateway()

	assert.EventuallyEqual(t, e.listenerConditions, listenerResolved)
	assert.EventuallyEqual(t, e.gatewayCredential, resolvedCredential)

	// Deleting the bundle must drop the listener rather than leave stale trust anchors.
	e.bundles.Delete(ctbName, "")
	assert.EventuallyEqual(t, e.listenerConditions, listenerInvalidRef)
	assert.EventuallyEqual(t, e.gatewayCredential, noCredential)

	e.bundles.Create(newTestBundle(ca2))
	assert.EventuallyEqual(t, e.listenerConditions, listenerResolved)
	assert.EventuallyEqual(t, e.gatewayCredential, resolvedCredential)
}

func TestClusterTrustBundleTransientDiscoveryError(t *testing.T) {
	// The API is served, but discovery fails a few times at startup. That is not an answer: nothing may be reported as
	// "not served" (InvalidKind) in the meantime.
	e := newCTBEnv(t, true, withBundle(newTestBundle(clustertrustbundletest.GenerateCA(t, "ca"))),
		withFailingDiscovery(3), func(e *ctbEnv) {
			e.createBackendTLSPolicy()
			e.createGateway()
		})

	assert.EventuallyEqual(t, e.policyConditions, policyResolved)
	assert.EventuallyEqual(t, e.destinationRuleCredential, resolvedCredential)
	assert.EventuallyEqual(t, e.listenerConditions, listenerResolved)
	assert.EventuallyEqual(t, e.gatewayCredential, resolvedCredential)
	assert.Equal(t, e.sq.seen("ResolvedRefs=True/ResolvedRefs"), true)
	assert.Equal(t, e.sq.seen("InvalidKind"), false)
	assert.Equal(t, e.sq.seen("InvalidCACertificateKind"), false)
}

func (e *ctbEnv) createConfigMapPolicy() {
	w := clienttest.NewWriter[*corev1.Service](e.t, e.kc)
	w.Create(&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "default"}, Spec: corev1.ServiceSpec{
		Ports: []corev1.ServicePort{{Name: "http", Port: 80}},
	}})
	clienttest.NewWriter[*corev1.ConfigMap](e.t, e.kc).Create(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "ca", Namespace: "default"},
		Data:       map[string]string{"ca.crt": clustertrustbundletest.GenerateCA(e.t, "configmap-ca")},
	})
	clienttest.NewWriter[*k8s.BackendTLSPolicy](e.t, e.kc).Create(&k8s.BackendTLSPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "policy-cm", Namespace: "default"},
		Spec: k8s.BackendTLSPolicySpec{
			TargetRefs: []k8s.LocalPolicyTargetReferenceWithSectionName{{
				LocalPolicyTargetReference: k8s.LocalPolicyTargetReference{Kind: "Service", Name: "other"},
			}},
			Validation: k8s.BackendTLSPolicyValidation{
				CACertificateRefs: []k8s.LocalObjectReference{{Kind: "ConfigMap", Name: "ca"}},
				Hostname:          "auth.example.com",
			},
		},
	})
}

func TestClusterTrustBundleUnreadable(t *testing.T) {
	test.SetForTest(t, &clustertrustbundle.InformerSyncTimeout, 200*time.Millisecond)
	forbidden := atomic.NewBool(true)
	e := newCTBEnv(t, true, withBundle(newTestBundle(clustertrustbundletest.GenerateCA(t, "ca"))), withBundleListForbidden(forbidden))
	e.createBackendTLSPolicy()
	e.createGateway()

	// An unreadable bundle is an invalid reference (not an unsupported kind), fail closed, and says why.
	assert.EventuallyEqual(t, e.policyConditions, policyInvalidRef)
	assert.EventuallyEqual(t, e.destinationRuleCredential, invalidCredential)
	if msg := e.policyMessage("ResolvedRefs"); !strings.Contains(msg, "cannot list/watch clustertrustbundles.certificates.k8s.io") {
		t.Fatalf("message %q does not point at RBAC", msg)
	}
	assert.EventuallyEqual(t, e.listenerConditions, listenerInvalidRef)
	assert.Equal(t, e.gatewayCredential(), noCredential)

	// Everything else keeps reconciling.
	e.createConfigMapPolicy()
	assert.EventuallyEqual(t, e.policyConditionsOf("policy-cm"), policyResolved)

	// Once istiod is allowed to list, the references converge without a restart. The reflector backs off between attempts.
	forbidden.Store(false)
	slow := retry.Timeout(30 * time.Second)
	assert.EventuallyEqual(t, e.policyConditions, policyResolved, slow)
	assert.EventuallyEqual(t, e.destinationRuleCredential, resolvedCredential, slow)
	assert.EventuallyEqual(t, e.listenerConditions, listenerResolved, slow)
	assert.EventuallyEqual(t, e.gatewayCredential, resolvedCredential, slow)
	assert.EventuallyEqual(t, e.policyConditionsOf("policy-cm"), policyResolved)
}
