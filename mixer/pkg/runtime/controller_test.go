// Copyright 2017 Istio Authors
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

package runtime

import (
	"errors"
	"flag"
	"fmt"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes/wrappers"

	"istio.io/mixer/pkg/adapter"
	adptTmpl "istio.io/mixer/pkg/adapter/template"
	cpb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/config/store"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/template"
)

type fakedispatcher struct {
	called int
}

func (f *fakedispatcher) ChangeResolver(rt Resolver) {
	f.called++
}

func TestControllerEmpty(t *testing.T) {
	d := &fakedispatcher{}
	c := &Controller{
		adapterInfo:            make(map[string]*adapter.Info),
		templateInfo:           make(map[string]template.Info),
		eval:                   nil,
		configState:            make(map[store.Key]*store.Resource),
		dispatcher:             d,
		resolver:               &resolver{}, // get an empty resolver
		identityAttribute:      DefaultIdentityAttribute,
		defaultConfigNamespace: DefaultConfigNamespace,
		createHandlerFactory: func(templateInfo map[string]template.Info, expr expr.TypeChecker,
			df expr.AttributeDescriptorFinder, builderInfo map[string]*adapter.Info) HandlerFactory {
			return &fhbuilder{}
		},
	}
	c.publishSnapShot()
	if d.called != 1 {
		t.Fatalf("dispatcher was not notified")
	}
	if len(c.resolver.rules) != 0 {
		t.Fatalf("rules. got %d, want 0", len(c.resolver.rules))
	}
}

type fhandler struct {
	name       string
	closed     bool
	closeError error
}

func (f *fhandler) Close() error {
	f.closed = true
	return f.closeError
}

type fhbuilder struct {
	a      *fhandler
	err    error
	h      *cpb.Handler
	inst   []*cpb.Instance
	called int
}

func (f *fhbuilder) Build(h *cpb.Handler, inst []*cpb.Instance, env adapter.Env) (adapter.Handler, error) {
	f.called++
	f.h = h
	f.inst = inst
	f.a.name = h.Name
	if f.err != nil {
		return nil, f.err
	}
	return f.a, f.err
}

func checkActionInvariants(t *testing.T, act *Action) {
	templateSet := make(map[string]bool)
	for _, ic := range act.instanceConfig {
		templateSet[ic.Template] = true
	}

	if len(templateSet) > 1 {
		t.Fatalf("actions contract violated, action have instances from %d templates", len(templateSet))
	}
}

func checkRulesInvariants(t *testing.T, rules rulesListByNamespace) {
	for _, ruleArr := range rules {
		for _, r := range ruleArr {
			for _, vr := range r.actions {
				for _, a := range vr {
					checkActionInvariants(t, a)
				}
			}
		}
	}
}

func TestController_workflow(t *testing.T) {
	mcd := maxCleanupDuration
	defer func() { maxCleanupDuration = mcd }()

	adapterInfo := map[string]*adapter.Info{
		"AA": {
			Name: "AA",
		},
	}
	templateInfo := map[string]template.Info{
		"metric": {
			Name: "metric",
		},
	}
	configState := map[store.Key]*store.Resource{
		{RulesKind, DefaultConfigNamespace, "r1"}: {Spec: &cpb.Rule{
			Match: "target.service == \"abc\"",
			Actions: []*cpb.Action{
				{
					Handler:   "a1.AA." + DefaultConfigNamespace,
					Instances: []string{"m1.metric." + DefaultConfigNamespace},
				},
			},
		},
		},
		{"metric", DefaultConfigNamespace, "m1"}: {Spec: &wrappers.StringValue{Value: "metric1_config"}},
		{"AA", DefaultConfigNamespace, "a1"}:     {Spec: &wrappers.StringValue{Value: "AA_config"}},
	}

	d := &fakedispatcher{}
	hndlr := &fhandler{name: "aa"}
	fb := &fhbuilder{a: hndlr}
	res := &resolver{refCount: 1}
	c := &Controller{
		adapterInfo:            adapterInfo,
		templateInfo:           templateInfo,
		eval:                   nil,
		configState:            configState,
		dispatcher:             d,
		resolver:               res, // get an empty resolver
		identityAttribute:      DefaultIdentityAttribute,
		defaultConfigNamespace: DefaultConfigNamespace,
		createHandlerFactory: func(templateInfo map[string]template.Info, expr expr.TypeChecker,
			df expr.AttributeDescriptorFinder, builderInfo map[string]*adapter.Info) HandlerFactory {
			return fb
		},
	}

	go func() {
		res.decRefCount()
	}()

	c.publishSnapShot()

	// check what was called.
	if fb.called != 1 {
		t.Fatalf("handler called: %d, want 1", fb.called)
	}
	hname := "a1.AA.istio-config-default"
	if fb.h.Name != hname {
		t.Fatalf("got %s, want %s handler", fb.h.Name, hname)
	}
	if len(fb.inst) != 1 {
		t.Fatalf("got %d, want 1 instance", len(fb.inst))
	}

	if hndlr.closed {
		t.Fatalf("handler was incorrectly closed")
	}

	// change config.

	events := []*store.Event{
		{
			Key:   store.Key{"metric", DefaultConfigNamespace, "m2"},
			Value: &store.Resource{Spec: &wrappers.StringValue{Value: "metric2_config"}},
		},
		{
			Key: store.Key{RulesKind, DefaultConfigNamespace, "r2"},
			Value: &store.Resource{Spec: &cpb.Rule{
				Match: "target.service == \"bcd\"",
				Actions: []*cpb.Action{
					{
						Handler:   "a1.AA." + DefaultConfigNamespace,
						Instances: []string{"m2.metric." + DefaultConfigNamespace, "BadInstance"},
					},
					{
						Handler:   "badhandler",
						Instances: []string{"m2.metric." + DefaultConfigNamespace},
					},
				},
			},
			},
		},
	}
	c.applyEvents(events)

	// check what was called.
	if fb.called != 2 {
		t.Fatalf("handler create called: %d, want 1", fb.called)
	}
	hname = "a1.AA.istio-config-default"
	if fb.h.Name != hname {
		t.Fatalf("got %s, want %s handler", fb.h.Name, hname)
	}
	if len(fb.inst) != 2 {
		t.Fatalf("got %d, want 2 instance", len(fb.inst))
	}

	if !hndlr.closed {
		t.Fatalf("handler was not closed")
	}

	c.applyEvents([]*store.Event{
		{Key: store.Key{"AA", DefaultConfigNamespace, "a1"}, Type: store.Delete},
	})

	// check what was called.
	if fb.called != 2 {
		t.Fatalf("handler create called: %d, want 1", fb.called)
	}
	hname = "a1.AA.istio-config-default"
	if fb.h.Name != hname {
		t.Fatalf("got %s, want %s handler", fb.h.Name, hname)
	}
	if len(fb.inst) != 2 {
		t.Fatalf("got %d, want 2 instance", len(fb.inst))
	}

	if !hndlr.closed {
		t.Fatalf("handler was not closed")
	}

	if c.nrules > 0 {
		t.Fatalf("got %d rules, want %d", c.nrules, 0)
	}

	checkRulesInvariants(t, c.resolver.rules)
}

func Test_cleanupResolver(t *testing.T) {
	cr := cleanupSleepTime
	cleanupSleepTime = 50 * time.Millisecond
	r := &resolver{}
	r.refCount = 2

	// force a timeout by not reducing refcount.
	err := cleanupResolver(r, nil, 2*cleanupSleepTime)
	if err == nil {
		t.Fatalf("resolver did not error")
	}

	table := map[string]*HandlerEntry{
		"h1": {
			Handler:        &fhandler{name: "h1"},
			closeOnCleanup: true,
		},
		"h2": {
			Handler:        &fhandler{name: "h2"},
			closeOnCleanup: false,
		},
		"h3": {
			Handler:        &fhandler{name: "h3", closeError: errors.New("unable to close")},
			closeOnCleanup: true,
		},
	}

	done := make(chan bool)
	go func() {
		if err := cleanupResolver(r, table, 5*cleanupSleepTime); err != nil {
			done <- false
		} else {
			done <- true
		}
	}()

	r.decRefCount()
	r.decRefCount()
	ok := false

	tc := time.NewTimer(4 * cleanupSleepTime).C
	select {
	case ok = <-done:
	case <-tc:
		t.Fatalf("did not cleanup in time.")
	}

	if !ok {
		t.Fatalf("cleanup did not finish")
	}

	for _, he := range table {
		hh := he.Handler.(*fhandler)
		if hh.closed != he.closeOnCleanup {
			t.Fatalf("closing got %t, want %t", hh.closed, he.closeOnCleanup)
		}
	}

	cleanupSleepTime = cr
}

func waitFor(t *testing.T, tm time.Duration, done chan bool, msg string) bool {
	tc := time.NewTimer(tm).C
	ok := false
	select {
	case ok = <-done:
	case <-tc:
		t.Fatalf("time out waiting for %s", msg)
	}

	if !ok {
		t.Fatal(msg)
	}
	return ok
}

func Test_WaitForChanges(t *testing.T) {
	wd := watchFlushDuration

	watchFlushDuration = 200 * time.Millisecond

	wch := make(chan store.Event)
	done := make(chan bool)
	//ok := false

	nevents := 2

	go watchChanges(wch, func(events []*store.Event) {
		if len(events) == nevents {
			done <- true
		} else {
			done <- false
		}
	})

	wch <- store.Event{}
	wch <- store.Event{}
	waitFor(t, 2*watchFlushDuration, done, "changes did not appear")

	nevents = 1
	wch <- store.Event{}
	waitFor(t, 2*watchFlushDuration, done, "changes did not appear")

	watchFlushDuration = wd
}

func TestAttributeFinder_GetAttribute(t *testing.T) {
	c := &Controller{}

	c.configState = map[store.Key]*store.Resource{
		{AttributeManifestKind, DefaultConfigNamespace, "at1"}: {Spec: &cpb.AttributeManifest{
			Name: "k8s",
			Attributes: map[string]*cpb.AttributeManifest_AttributeInfo{
				"a": {},
				"b": {},
			},
		}},
		{AttributeManifestKind, DefaultConfigNamespace, "at2"}: {Spec: &cpb.AttributeManifest{
			Name: "k8s",
			Attributes: map[string]*cpb.AttributeManifest_AttributeInfo{
				"c": {},
				"d": {},
			},
		}},
		{"unknownKind", DefaultConfigNamespace, "at2"}: {Spec: &cpb.AttributeManifest{}},
	}

	df := c.processAttributeManifests()

	for _, tc := range []struct {
		aname string
		found bool
	}{
		{"a", true},
		{"b", true},
		{"c", true},
		{"d", true},
		{"e", false},
		{"f", false},
	} {
		t.Run(tc.aname, func(t *testing.T) {

			att := df.GetAttribute(tc.aname)
			found := att != nil
			if found != tc.found {
				t.Fatalf("attribute %s got found=%t, want found=%t", tc.aname, found, tc.found)
			}
		})
	}
}

func TestController_Resolve2(t *testing.T) {
	handlerName := "h1"
	rc := func() rulesMapByNamespace {
		return rulesMapByNamespace{
			"ns1": rulesByName{
				"r1": &Rule{
					selector: "true",
					name:     "r1",
					actions: map[adptTmpl.TemplateVariety][]*Action{
						adptTmpl.TEMPLATE_VARIETY_CHECK: {
							&Action{
								handlerName: handlerName,
							},
						},
					},
				},
			},
		}
	}

	for _, tc := range []struct {
		desc     string
		ht       map[string]*HandlerEntry
		numRules int
	}{
		{"empty_table", nil, 0},
		{"uninitialized_handler", map[string]*HandlerEntry{
			handlerName: {},
		}, 0},
		{"good_handler", map[string]*HandlerEntry{
			handlerName: {Handler: &fhandler{}},
		}, 1},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			_, n := generateResolvedRules(rc(), tc.ht)
			if n != tc.numRules {
				t.Fatalf("nrules got: %d, want %d", n, tc.numRules)
			}

		})
	}
}

func TestController_ResourceType(t *testing.T) {
	for _, tc := range []struct {
		labels map[string]string
		rt     ResourceType
	}{
		{labels: map[string]string{
			istioProtocol: "tcp",
		}, rt: ResourceType{protocolTCP, methodCheck | methodReport | methodPreprocess}},
		{labels: map[string]string{
			istioProtocol: "http",
		}, rt: ResourceType{protocolHTTP, methodCheck | methodReport | methodPreprocess}},
		{labels: nil, rt: ResourceType{protocolHTTP, methodCheck | methodReport | methodPreprocess}},
	} {
		t.Run(fmt.Sprintf("%v", tc.labels), func(t *testing.T) {
			rt := resourceType(tc.labels)

			if rt != tc.rt {
				t.Fatalf("got %v, want %v", rt, tc.rt)
			}
		})
	}

}

//unc canonicalizeInstanceNames(instances []string, namespace string) []string
func TestController_canInstances(t *testing.T) {
	ns := "default-ns"
	for _, tc := range []struct {
		desc  string
		insts []string
	}{
		{"fdqnInstance", []string{
			"n1.kind1." + ns,
		}},
		{"nonFqdnHandler", []string{
			"n1.kind1",
		}},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			insts := canonicalizeInstanceNames(tc.insts, ns)
			for _, inst := range insts {
				if !isFQN(inst) {
					t.Fatalf("name was not canonicalized: %s", inst)
				}
			}
		})
	}
}

func TestController_canHandlers(t *testing.T) {
	ns := "default-ns"
	for _, tc := range []struct {
		desc string
		acts []*cpb.Action
	}{
		{"fdqnHandler", []*cpb.Action{
			{Handler: "n1.kind1." + ns},
		}},
		{"nonFqdnHandler", []*cpb.Action{
			{Handler: "n1.kind1"},
		}},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			act := canonicalizeHandlerNames(tc.acts, ns)
			for _, a := range act {
				if !isFQN(a.Handler) {
					t.Fatalf("name was not canonicalized: %s", a.Handler)
				}
			}
		})
	}
}

var _ = flag.Lookup("v").Value.Set("99")
var _ = flag.Lookup("logtostderr").Value.Set("true")
