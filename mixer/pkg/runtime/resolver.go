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
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"

	adptTmpl "istio.io/mixer/pkg/adapter/template"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/expr"
)

// Rule represents a runtime view of cpb.Rule.
type Rule struct {
	// Selector from the original rule.
	selector string
	// Actions are stored in runtime format.
	actions map[adptTmpl.TemplateVariety][]*Action
	// Rule is a top level config object and it has a unique name.
	// It is used here for informational purposes.
	name string
	// rtype is gathered from labels.
	rtype ResourceType
}

// resolver is the runtime view of the configuration database.
type resolver struct {
	// evaluator evaluates selectors
	evaluator expr.PredicateEvaluator

	// identityAttribute defines which configuration scopes apply to a request.
	// default: target.service
	// The value of this attribute is expected to be a hostname of form "svc.$ns.suffix"
	identityAttribute string

	// defaultConfigNamespace defines the namespace that contains configuration defaults for istio.
	// This is distinct from the "default" namespace in K8s.
	// default: istio-default-config
	defaultConfigNamespace string

	// rules in the configuration database keyed by $namespace.
	rules map[string][]*Rule

	// refCount tracks the number requests currently using this
	// configuration. resolver state can be cleaned up when this count is 0.
	refCount int32

	// id of the resolver for debugging.
	id int
}

// newResolver returns a Resolver.
func newResolver(evaluator expr.PredicateEvaluator, identityAttribute string, defaultConfigNamespace string,
	rules map[string][]*Rule, id int) *resolver {
	return &resolver{
		evaluator:              evaluator,
		identityAttribute:      identityAttribute,
		defaultConfigNamespace: defaultConfigNamespace,
		rules: rules,
		id:    id,
	}
}

// DefaultConfigNamespace holds istio wide configuration.
const DefaultConfigNamespace = "istio-config-default"

// DefaultIdentityAttribute is attribute that defines config scopes.
const DefaultIdentityAttribute = "target.service"

// ContextProtocolAttributeName is the attribute that defines the protocol context.
const ContextProtocolAttributeName = "context.protocol"

// expectedResolvedActionsCount is used to preallocate slice for actions.
const expectedResolvedActionsCount = 10

// Resolve resolves the in memory configuration to a set of actions based on request attributes.
// Resolution is performed in the following order
// 1. Check rules from the defaultConfigNamespace -- these rules always apply
// 2. Check rules from the target.service namespace
func (r *resolver) Resolve(attrs attribute.Bag, variety adptTmpl.TemplateVariety) (ra Actions, err error) {
	nselected := 0
	target := "unknown"

	start := time.Now()
	// increase refcount just before returning
	// only if there is no error.
	defer func() {
		if err == nil {
			r.incRefCount()
		}
	}()

	// monitoring info
	defer func() {
		lbls := prometheus.Labels{
			targetStr: target,
			errorStr:  strconv.FormatBool(err != nil),
		}
		resolveCounter.With(lbls).Inc()
		resolveDuration.With(lbls).Observe(time.Since(start).Seconds())
		resolveRules.With(lbls).Observe(float64(nselected))
		raLen := 0
		if ra != nil {
			raLen = len(ra.Get())
		}
		resolveActions.With(lbls).Observe(float64(raLen))
	}()

	attr, _ := attrs.Get(r.identityAttribute)
	if attr == nil {
		msg := fmt.Sprintf("%s identity not found in attributes%v", r.identityAttribute, attrs.Names())
		glog.Warningf(msg)
		return nil, errors.New(msg)
	}

	rulesArr := make([][]*Rule, 0, 2)

	// add default namespace if present
	if dcf := r.rules[r.defaultConfigNamespace]; dcf != nil {
		rulesArr = append(rulesArr, dcf)
	} else if glog.V(3) {
		glog.Infof("Resolve: no namespace config for %s", r.defaultConfigNamespace)
	}

	target = attr.(string)
	// add service namespace if present
	splits := strings.SplitN(target, ".", 3) // we only care about service and namespace.
	if len(splits) > 1 {
		ns := splits[1]
		if dcf := r.rules[ns]; dcf != nil {
			rulesArr = append(rulesArr, dcf)
		} else if glog.V(4) {
			glog.Infof("Resolve: no namespace config for %s. target: %s.", ns, target)
		}
	}
	var res []*Action
	res, nselected, err = r.filterActions(rulesArr, attrs, variety)

	if err != nil {
		return nil, err
	}

	// TODO add dedupe + group actions by handler/template

	ra = &actions{a: res, done: r.decRefCount}
	return ra, nil
}

//filterActions filters rules based on template variety and selectors.
func (r *resolver) filterActions(rulesArr [][]*Rule, attrs attribute.Bag,
	variety adptTmpl.TemplateVariety) ([]*Action, int, error) {
	res := make([]*Action, 0, expectedResolvedActionsCount)
	var selected bool
	nselected := 0
	var err error
	ctxProtocol, _ := attrs.Get(ContextProtocolAttributeName)
	tcp := ctxProtocol == "tcp"

	for _, rules := range rulesArr {
		for _, rule := range rules {
			act := rule.actions[variety]
			if act == nil { // do not evaluate selector if there is no variety specific action there.
				continue
			}
			// default rtype is HTTP + Check|Report|Preprocess
			if tcp != rule.rtype.IsTCP() {
				if glog.V(4) {
					glog.Infof("filterActions: rule %s removed ctxProtocol=%s, type %s", rule.name, ctxProtocol, rule.rtype)
				}
				continue
			}

			// do not evaluate empty predicates.
			if len(rule.selector) != 0 {
				if selected, err = r.evaluator.EvalPredicate(rule.selector, attrs); err != nil {
					return nil, 0, err
				}
				if !selected {
					continue
				}
			}
			if glog.V(3) {
				glog.Infof("filterActions: rule %s selected %v", rule.name, rule.rtype)
			}
			nselected++
			res = append(res, act...)
		}
	}
	return res, nselected, nil
}

func (r *resolver) incRefCount() {
	atomic.AddInt32(&r.refCount, 1)
}

func (r *resolver) decRefCount() {
	atomic.AddInt32(&r.refCount, -1)
}

// actions implements Actions interface.
type actions struct {
	a    []*Action
	done func()
}

func (a *actions) Get() []*Action {
	return a.a
}

func (a *actions) Done() {
	a.done()
}
