// Copyright 2017 Google Inc.
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
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/expr"

	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"
	pb "istio.io/mixer/pkg/config/proto"
)

type (
	// Runtime represents the runtime view of the config.
	// It is pre-validated and immutable.
	// It can be safely used concurrently.
	Runtime struct {
		Validated
		// used to evaluate selectors
		eval expr.PredicateEvaluator
	}
	// Combined config is given to aspect managers.
	Combined struct {
		Builder *pb.Adapter
		Aspect  *pb.Aspect
	}

	// AspectSet is a set of aspects. ex: Check call will result in {"listChecker", "iam"}
	// Runtime should only return aspects matching a certain type.
	AspectSet map[string]bool
)

// NewRuntime returns a Runtime object given a validated config and a predicate eval.
func NewRuntime(v *Validated, evaluator expr.PredicateEvaluator) *Runtime {
	return &Runtime{
		Validated: *v,
		eval:      evaluator,
	}
}

// Resolve returns a list of CombinedConfig given an attribute bag.
// It will only return config from the requested set of aspects.
// For example the Check handler and Report handler will request
// a disjoint set of aspects check: {iplistChecker, iam}, report: {Log, metrics}
func (r *Runtime) Resolve(bag attribute.Bag, aspectSet AspectSet) ([]*Combined, error) {
	dlist := make([]*Combined, 0, r.numAspects)
	err := r.resolveRules(bag, aspectSet, r.serviceConfig.GetRules(), "/", &dlist)
	return dlist, err
}

func (r *Runtime) evalPredicate(selector string, bag attribute.Bag) (bool, error) {
	// empty selector always selects
	if selector == "" {
		return true, nil
	}
	return r.eval.EvalPredicate(selector, bag)
}

// resolveRules recurses thru the config struct and returns a list of combined aspects
func (r *Runtime) resolveRules(bag attribute.Bag, aspectSet AspectSet, rules []*pb.AspectRule, path string, dlist *[]*Combined) (err error) {
	var selected bool
	var lerr error

	for _, rule := range rules {
		if glog.V(2) {
			glog.Infof("resolveRules (%v) ==> %v ", rule, path)
		}
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
			if cs := r.combined(aa, aspectSet); cs != nil {
				*dlist = append(*dlist, cs)
			}
		}
		rs := rule.GetRules()
		if len(rs) == 0 {
			continue
		}
		if lerr = r.resolveRules(bag, aspectSet, rs, path, dlist); lerr != nil {
			err = multierror.Append(err, lerr)
		}
	}
	return err
}

// combined returns a Combined config given an aspect config
func (r *Runtime) combined(aa *pb.Aspect, aspectSet AspectSet) *Combined {
	if !aspectSet[aa.GetKind()] {
		glog.V(3).Infof("Aspect Rejected %v not is set (%v)", aa.GetKind(), aspectSet)
		return nil
	}

	var adp *pb.Adapter
	// find matching adapter
	// assume that config references are correct
	if aa.GetAdapter() != "" {
		adp = r.adapterByName[aa.GetAdapter()]
	} else {
		adp = r.adapterByKind[aa.GetKind()][0]
	}

	return &Combined{adp, aa}
}
