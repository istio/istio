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
	}
)

// newRuntime returns a runtime object given a validated config and a predicate eval.
func newRuntime(v *Validated, evaluator expr.PredicateEvaluator) *runtime {
	return &runtime{
		Validated: *v,
		eval:      evaluator,
	}
}

// resolve returns a list of CombinedConfig given an attribute bag.
// It will only return config from the requested set of aspects.
// For example the Check handler and Report handler will request
// a disjoint set of aspects check: {iplistChecker, iam}, report: {Log, metrics}
func (r *runtime) Resolve(bag attribute.Bag, kindSet KindSet) (dlist []*pb.Combined, err error) {
	if glog.V(2) {
		glog.Infof("resolving for: %s", kindSet)
		defer func() { glog.Infof("resolved (err=%v): %s", err, dlist) }()
	}
	dlist = make([]*pb.Combined, 0, r.numAspects)
	return r.resolveRules(bag, kindSet, r.serviceConfig.GetRules(), "/", dlist)
}

func (r *runtime) evalPredicate(selector string, bag attribute.Bag) (bool, error) {
	// empty selector always selects
	if selector == "" {
		return true, nil
	}
	return r.eval.EvalPredicate(selector, bag)
}

// resolveRules recurses through the config struct and returns a list of combined aspects
func (r *runtime) resolveRules(bag attribute.Bag, kindSet KindSet, rules []*pb.AspectRule, path string, dlist []*pb.Combined) ([]*pb.Combined, error) {
	var selected bool
	var lerr error
	var err error

	for _, rule := range rules {
		glog.V(3).Infof("resolveRules (%v) ==> %v ", rule, path)

		sel := rule.GetSelector()
		if selected, lerr = r.evalPredicate(sel, bag); lerr != nil {
			err = multierror.Append(err, lerr)
			continue
		}
		if !selected {
			continue
		}
		path = path + "/" + sel
		for _, aa := range rule.GetAspects() {
			k, ok := ParseKind(aa.Kind)
			if !ok || !kindSet.IsSet(k) {
				glog.V(3).Infof("Aspect %s not selected [%v]", aa.Kind, kindSet)
				continue
			}
			adp := r.adapterByName[adapterKey{k, aa.Adapter}]
			glog.V(2).Infof("selected aspect %s -> %s", aa.Kind, adp)
			dlist = append(dlist, &pb.Combined{Builder: adp, Aspect: aa})
		}
		rs := rule.GetRules()
		if len(rs) == 0 {
			continue
		}
		if dlist, lerr = r.resolveRules(bag, kindSet, rs, path, dlist); lerr != nil {
			err = multierror.Append(err, lerr)
		}
	}
	return dlist, err
}
