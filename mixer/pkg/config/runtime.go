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

package config

import (
	"fmt"
	"strings"

	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"

	"istio.io/mixer/pkg/attribute"
	pb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
)

type (
	// runtime represents the runtime view of the config.
	// It is pre-validated and immutable.
	// It can be safely used concurrently.
	runtime struct {
		Validated
		// used to evaluate selectors
		eval expr.PredicateEvaluator

		// config is organized around the identityAttribute.
		identityAttribute       string
		identityAttributeDomain string
	}
)

// newRuntime returns a runtime object given a validated config and a predicate eval.
func newRuntime(v *Validated, evaluator expr.PredicateEvaluator, identityAttribute, identityAttributeDomain string) *runtime {
	return &runtime{
		Validated:               *v,
		eval:                    evaluator,
		identityAttribute:       identityAttribute,
		identityAttributeDomain: identityAttributeDomain,
	}
}

// GetScopes returns configuration scopes that apply given a target service
// k8s example:  domain maps to global
// attr - my-svc.my-namespace.svc.cluster.local
// domain - svc.cluster.local
// --> "global", "my-namespace.svc.cluster.local", "my-svc.my-namespace.svc.cluster.local"
// ordering of scopes is from least specific to most specific.
func GetScopes(attr string, domain string, scopes []string) ([]string, error) {
	if !strings.HasSuffix(attr, domain) {
		return scopes, fmt.Errorf("internal error: scope %s not in %s", attr, domain)
	}
	scopes = append(scopes, global)
	for idx := len(attr) - (len(domain) + 2); idx >= 0; idx-- {
		if attr[idx] == '.' {
			scopes = append(scopes, attr[idx+1:])
			idx--
		}
	}
	scopes = append(scopes, attr)
	return scopes, nil
}

// Resolve returns a list of CombinedConfig given an attribute bag.
// It will only return config from the requested set of aspects.
// For example the Check handler and Report handler will request
// a disjoint set of aspects check: {iplistChecker, iam}, report: {Log, metrics}
// If strict is true, resolution will fail if any selector evaluations fail; otherwise it will log errors and return
// as much config as we were able to resolve.
func (r *runtime) Resolve(bag attribute.Bag, set KindSet, strict bool) (dlist []*pb.Combined, err error) {
	if glog.V(4) {
		glog.Infof("resolving for kinds: %s", set)
		defer func() {
			builders := make([]string, len(dlist))
			for i, o := range dlist {
				if o.Builder != nil {
					builders[i] = o.Builder.Impl
				}
			}
			glog.Infof("resolved configs (err=%v): %v", err, builders)
		}()
	}
	return resolve(
		bag,
		set,
		r.rule,
		r.resolveRules,
		false, /* conditional full resolve */
		r.identityAttribute,
		r.identityAttributeDomain,
		strict)
}

// ResolveUnconditional returns the list of CombinedConfigs for the supplied
// attributes and kindset based on resolution of unconditional rules. That is,
// it only attempts to find aspects in rules that have an empty selector. This
// method is primarily used for pre-process aspect configuration retrieval.
// If strict is true, resolution will fail if any selector evaluations fail; otherwise it will log errors and return
// as much config as we were able to resolve.
func (r *runtime) ResolveUnconditional(bag attribute.Bag, set KindSet, strict bool) (out []*pb.Combined, err error) {
	if glog.V(2) {
		glog.Infof("unconditionally resolving for kinds: %s", set)
		defer func() {
			builders := make([]string, len(out))
			for i, o := range out {
				if o.Builder != nil {
					builders[i] = o.Builder.Impl
				}
			}
			glog.Infof("unconditionally resolved configs (err=%v): %v", err, builders)
		}()
	}
	return resolve(
		bag,
		set,
		r.rule,
		r.resolveRules,
		true, /* unconditional resolve */
		r.identityAttribute,
		r.identityAttributeDomain,
		strict)
}

// Make this a reasonable number so that we don't reallocate slices often.
const resolveSize = 50

// resolve - the main config resolution function.
func resolve(bag attribute.Bag, kindSet KindSet, rules map[rulesKey]*pb.ServiceConfig, resolveRules resolveRulesFunc,
	onlyEmptySelectors bool, identityAttribute string, identityAttributeDomain string, strictSelectorEval bool) (dlist []*pb.Combined, err error) {
	scopes := make([]string, 0, 10)

	attr, _ := bag.Get(identityAttribute)
	if attr == nil {
		// it is ok for identity attributes to be absent
		// during pre processing. since global scope always applies
		// set it to that.
		if onlyEmptySelectors {
			scopes = []string{global}
		} else {
			glog.Warningf("%s attribute not found in %p", identityAttribute, bag)
			return nil, fmt.Errorf("%s attribute not found", identityAttribute)
		}
	} else {
		attrStr, ok := attr.(string)
		if !ok {
			return nil, fmt.Errorf("%s attribute with value %v should be of type string", identityAttribute, attr)
		}
		if scopes, err = GetScopes(attrStr, identityAttributeDomain, scopes); err != nil {
			return nil, err
		}
	}

	dlist = make([]*pb.Combined, 0, resolveSize)
	dlistout := make([]*pb.Combined, 0, resolveSize)

	for idx := 0; idx < len(scopes); idx++ {
		scope := scopes[idx]
		amap := make(map[string][]*pb.Combined)

		for j := idx; j < len(scopes); j++ {
			subject := scopes[j]
			key := rulesKey{scope, subject}
			rule := rules[key]
			if rule == nil {
				glog.V(2).Infof("no rules for %s", key)
				continue
			}
			// empty the slice, do not re allocate
			dlist = dlist[:0]
			if dlist, err = resolveRules(bag, kindSet, rule.GetRules(), "/", dlist, onlyEmptySelectors, strictSelectorEval); err != nil {
				return dlist, err
			}

			aamap := make(map[string][]*pb.Combined)

			for _, d := range dlist {
				aamap[d.Aspect.Kind] = append(aamap[d.Aspect.Kind], d)
			}

			// more specific subject replaces
			for k, v := range aamap {
				amap[k] = v
			}
		}
		// collapse from amap
		for _, v := range amap {
			dlistout = append(dlistout, v...)
		}
	}

	return dlistout, nil
}

func (r *runtime) evalPredicate(selector string, bag attribute.Bag) (bool, error) {
	// empty selector always selects
	if selector == "" {
		return true, nil
	}
	return r.eval.EvalPredicate(selector, bag)
}

type resolveRulesFunc func(bag attribute.Bag, kindSet KindSet, rules []*pb.AspectRule, path string,
	dlist []*pb.Combined, onlyEmptySelectors bool, strictSelectorEval bool) ([]*pb.Combined, error)

// resolveRules recurses through the config struct and returns a list of combined aspects. If `strictSelectorEval` is
// true we will return an error when we fail the evaluate a selector; when it's false we'll return a nil error and as
// many chunks of config as we were able to resolve.
func (r *runtime) resolveRules(bag attribute.Bag, kindSet KindSet, rules []*pb.AspectRule, path string,
	dlist []*pb.Combined, onlyEmptySelectors bool, strictSelectorEval bool) ([]*pb.Combined, error) {
	var selected bool
	var lerr error
	var err error

	for _, rule := range rules {
		// We write detailed logs about a single rule into a string rather than printing as we go to ensure
		// they're all in a single log line and not interleaved with logs from other requests.
		logMsg := ""
		if glog.V(3) {
			glog.Infof("resolveRules (%v) ==> %v ", rule.Selector, path)
		}

		sel := rule.GetSelector()
		if sel != "" && onlyEmptySelectors {
			continue
		}
		if selected, lerr = r.evalPredicate(sel, bag); lerr != nil {
			if glog.V(3) {
				glog.Infof("Failed to eval selector '%s': %v", sel, lerr)
			}
			if strictSelectorEval {
				err = multierror.Append(err, lerr)
			}
			continue
		}
		if !selected {
			continue
		}
		if glog.V(3) {
			logMsg = fmt.Sprintf("Rule (%v) applies, using aspect kinds %v:", rule.GetSelector(), kindSet)
		}

		path = path + "/" + sel
		for _, aa := range rule.GetAspects() {
			k, ok := ParseKind(aa.Kind)
			if !ok || !kindSet.IsSet(k) {
				if glog.V(3) {
					logMsg = fmt.Sprintf("%s\n- did not select aspect kind %s named '%s', wrong kind", logMsg, aa.Kind, aa.Adapter)
				}
				continue
			}
			adp := r.adapterByName[adapterKey{k, aa.Adapter}]
			if glog.V(3) {
				logMsg = fmt.Sprintf("%s\n- selected aspect kind %s with config: %v", logMsg, aa.Kind, adp)
			}
			dlist = append(dlist, &pb.Combined{Builder: adp, Aspect: aa})
		}

		if glog.V(3) {
			glog.Info(logMsg)
		}

		rs := rule.GetRules()
		if len(rs) == 0 {
			continue
		}
		if dlist, lerr = r.resolveRules(bag, kindSet, rs, path, dlist, onlyEmptySelectors, strictSelectorEval); lerr != nil {
			err = multierror.Append(err, lerr)
		}
	}
	return dlist, err
}
