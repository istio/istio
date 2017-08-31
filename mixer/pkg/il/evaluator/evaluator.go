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

package evaluator

import (
	"fmt"
	"net"

	"github.com/golang/glog"
	lru "github.com/hashicorp/golang-lru"

	pb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/config/descriptor"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/il/compiler"
	"istio.io/mixer/pkg/il/interpreter"
)

// IL is an implementation of expr.Evaluator that also exposes specific methods.
// Specifically, it can listen to config change events.
type IL struct {
	cache  *lru.Cache
	finder expr.AttributeDescriptorFinder
	fMap   map[string]expr.FuncBase
}

var _ expr.Evaluator = &IL{}
var _ config.ChangeListener = &IL{}

const ipFnName = "ip"

var ipExternFn = interpreter.ExternFromFn(ipFnName, func(in string) ([]byte, error) {
	if ip := net.ParseIP(in); ip != nil {
		return []byte(ip), nil
	}
	return []byte{}, fmt.Errorf("could not convert %s to IP_ADDRESS", in)
})

var externMap = map[string]interpreter.Extern{
	ipFnName: ipExternFn,
}

type cacheEntry struct {
	expression  *expr.Expression
	interpreter *interpreter.Interpreter
}

// Eval evaluates expr using the attr attribute bag and returns the result as interface{}.
func (e *IL) Eval(expr string, attrs attribute.Bag) (interface{}, error) {
	var result interpreter.Result
	var err error
	if result, err = e.evalResult(expr, attrs); err != nil {
		glog.Infof("evaluator.Eval failed expr:'%s', err: %v", expr, err)
		return nil, err
	}
	return result.AsInterface(), nil
}

// EvalString evaluates expr using the attr attribute bag and returns the result as string.
func (e *IL) EvalString(expr string, attrs attribute.Bag) (string, error) {
	var result interpreter.Result
	var err error
	if result, err = e.evalResult(expr, attrs); err != nil {
		glog.Infof("evaluator.EvalString failed expr:'%s', err: %v", expr, err)
		return "", err
	}
	return result.AsString(), nil
}

// EvalPredicate evaluates expr using the attr attribute bag and returns the result as bool.
func (e *IL) EvalPredicate(expr string, attrs attribute.Bag) (bool, error) {
	var result interpreter.Result
	var err error
	if result, err = e.evalResult(expr, attrs); err != nil {
		glog.Infof("evaluator.EvalPredicate failed expr:'%s', err: %v", expr, err)
		return false, err
	}
	return result.AsBool(), nil
}

// EvalType evaluates expr using the attr attribute bag and returns the type of the result.
func (e *IL) EvalType(expr string, finder expr.AttributeDescriptorFinder) (pb.ValueType, error) {
	var entry cacheEntry
	var err error
	if entry, err = e.getOrCreateCacheEntry(expr); err != nil {
		glog.Infof("evaluator.EvalType failed expr:'%s', err: %v", expr, err)
		return pb.VALUE_TYPE_UNSPECIFIED, err
	}

	return entry.expression.EvalType(finder, e.fMap)
}

// AssertType evaluates the type of expr using the attribute set; if the evaluated type is equal to
// the expected type we return nil, and return an error otherwise.
func (e *IL) AssertType(expr string, finder expr.AttributeDescriptorFinder, expectedType pb.ValueType) error {
	if t, err := e.EvalType(expr, finder); err != nil {
		return err
	} else if t != expectedType {
		return fmt.Errorf("expression '%s' evaluated to type %v, expected type %v", expr, t, expectedType)
	}
	return nil
}

// ChangeVocabulary handles changing of the attribute vocabulary.
func (e *IL) ChangeVocabulary(finder expr.AttributeDescriptorFinder) {
	e.finder = finder
	e.cache.Purge()
}

// ConfigChange handles changing of configuration.
func (e *IL) ConfigChange(cfg config.Resolver, df descriptor.Finder, handlers map[string]*config.HandlerInfo) {
	e.finder = df
	e.cache.Purge()
}

func (e *IL) evalResult(expr string, attrs attribute.Bag) (interpreter.Result, error) {
	var entry cacheEntry
	var err error
	if entry, err = e.getOrCreateCacheEntry(expr); err != nil {
		glog.Infof("evaluator.evalResult failed expr:'%s', err: %v", expr, err)
		return interpreter.Result{}, err
	}

	return entry.interpreter.Eval("eval", attrs)
}

func (e *IL) getOrCreateCacheEntry(expr string) (cacheEntry, error) {
	// TODO: add normalization for exprStr string, so that 'a | b' is same as 'a|b', and  'a == b' is same as 'b == a'

	if entry, found := e.cache.Get(expr); found {
		return entry.(cacheEntry), nil
	}

	if glog.V(4) {
		glog.Infof("expression cache miss for '%s'", expr)
	}

	var err error
	var result compiler.Result
	if result, err = compiler.Compile(expr, e.finder); err != nil {
		glog.Infof("evaluator.getOrCreateCacheEntry failed expr:'%s', err: %v", expr, err)
		return cacheEntry{}, err
	}

	if glog.V(4) {
		glog.Infof("caching expression for '%s''", expr)
	}

	intr := interpreter.New(result.Program, externMap)
	entry := cacheEntry{
		expression:  result.Expression,
		interpreter: intr,
	}

	_ = e.cache.Add(expr, entry)

	return entry, nil
}

// NewILEvaluator returns a new instance of IL.
func NewILEvaluator(cacheSize int) (*IL, error) {
	cache, err := lru.New(cacheSize)
	if err != nil {
		return nil, err
	}
	return &IL{
		cache: cache,
		fMap:  expr.FuncMap(),
	}, nil
}
