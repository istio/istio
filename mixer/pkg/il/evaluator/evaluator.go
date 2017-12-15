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
	"errors"
	"fmt"
	"sync"

	lru "github.com/hashicorp/golang-lru"

	pb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/expr"
	"istio.io/istio/mixer/pkg/il"
	"istio.io/istio/mixer/pkg/il/compiler"
	"istio.io/istio/mixer/pkg/il/interpreter"
	"istio.io/istio/mixer/pkg/il/runtime"
	"istio.io/istio/pkg/log"
)

// IL is an implementation of expr.Evaluator that also exposes specific methods.
// Specifically, it can listen to config change events.
type IL struct {
	cacheSize   int
	context     *attrContext
	contextLock sync.RWMutex
	functions   map[string]expr.FunctionMetadata
}

// attrContext captures the set of fields that needs to be kept & evicted together based on
// particular attribute metadata that was supplied as part of a ChangeListener call.
type attrContext struct {
	cache  *lru.Cache
	finder expr.AttributeDescriptorFinder
}

var _ expr.Evaluator = &IL{}

// Eval evaluates expr using the attr attribute bag and returns the result as interface{}.
func (e *IL) Eval(expr string, attrs attribute.Bag) (interface{}, error) {
	var result interpreter.Result
	var err error
	if result, err = e.evalResult(expr, attrs); err != nil {
		log.Infof("evaluator.Eval failed expr:'%s', err: %v", expr, err)
		return nil, err
	}
	return result.AsInterface(), nil
}

// EvalString evaluates expr using the attr attribute bag and returns the result as string.
func (e *IL) EvalString(expr string, attrs attribute.Bag) (string, error) {
	var result interpreter.Result
	var err error
	if result, err = e.evalResult(expr, attrs); err != nil {
		log.Infof("evaluator.EvalString failed expr:'%s', err: %v", expr, err)
		return "", err
	}
	return result.AsString(), nil
}

// EvalPredicate evaluates expr using the attr attribute bag and returns the result as bool.
func (e *IL) EvalPredicate(expr string, attrs attribute.Bag) (bool, error) {
	var result interpreter.Result
	var err error
	if result, err = e.evalResult(expr, attrs); err != nil {
		log.Infof("evaluator.EvalPredicate failed expr:'%s', err: %v", expr, err)
		return false, err
	}
	return result.AsBool(), nil
}

// EvalType evaluates expr using the attr attribute bag and returns the type of the result.
func (e *IL) EvalType(expression string, finder expr.AttributeDescriptorFinder) (pb.ValueType, error) {
	exp, err := expr.Parse(expression)
	if err != nil {
		return pb.VALUE_TYPE_UNSPECIFIED, err
	}

	return exp.EvalType(finder, e.functions)
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
	e.updateAttrContext(finder)
}

// updateAttrContext creates a new attrContext based on the supplied finder and a new cache, and replaces
// the old one atomically.
func (e *IL) updateAttrContext(finder expr.AttributeDescriptorFinder) {
	cache, err := lru.New(e.cacheSize)
	if err != nil {
		panic(fmt.Sprintf("Unexpected error from lru.New: %v", err))
	}

	context := &attrContext{
		cache:  cache,
		finder: finder,
	}

	e.contextLock.Lock()
	e.context = context
	e.contextLock.Unlock()
}

// getAttrContext gets the current attribute context atomically.
func (e *IL) getAttrContext() *attrContext {
	e.contextLock.RLock()
	defer e.contextLock.RUnlock()
	return e.context
}

func (e *IL) evalResult(expr string, attrs attribute.Bag) (interpreter.Result, error) {
	ctx := e.getAttrContext()
	return ctx.evalResult(expr, attrs, e.functions)
}

func (ctx *attrContext) evalResult(
	expr string,
	attrs attribute.Bag,
	functions map[string]expr.FunctionMetadata) (interpreter.Result, error) {

	var intr *interpreter.Interpreter
	var err error
	if intr, err = ctx.getOrCreateCacheEntry(expr, functions); err != nil {
		log.Infof("evaluator.evalResult failed expr:'%s', err: %v", expr, err)
		return interpreter.Result{}, err
	}

	r, err := intr.Eval("eval", attrs)
	return r, err
}

func (ctx *attrContext) getOrCreateCacheEntry(expr string, functions map[string]expr.FunctionMetadata) (*interpreter.Interpreter, error) {
	// TODO: add normalization for exprStr string, so that 'a | b' is same as 'a|b', and  'a == b' is same as 'b == a'
	if entry, found := ctx.cache.Get(expr); found {
		return entry.(*interpreter.Interpreter), nil
	}

	log.Debugf("expression cache miss for '%s'", expr)

	var err error
	var program *il.Program
	if program, err = compiler.Compile(expr, ctx.finder, functions); err != nil {
		log.Infof("evaluator.getOrCreateCacheEntry failed expr:'%s', err: %v", expr, err)
		return nil, err
	}

	log.Debugf("caching expression for '%s''", expr)

	intr := interpreter.New(program, allExterns)

	_ = ctx.cache.Add(expr, intr)

	return intr, nil
}

// DefaultCacheSize is the default size for the expression cache.
const DefaultCacheSize = 1024

// NewILEvaluator returns a new instance of IL.
func NewILEvaluator(cacheSize int) (*IL, error) {
	// check the cacheSize here, to ensure that we can ignore errors in lru.New calls.
	// cacheSize restriction is the only reason lru.New returns an error.
	if cacheSize <= 0 {
		return nil, errors.New("cacheSize must be positive")
	}

	return &IL{
		cacheSize: cacheSize,
		functions: allFunctions,
	}, nil
}

// TODO: This should be replaced with a common, shared context, instead of a singleton global.
var allFunctions = expr.FuncMap(runtime.ExternFunctionMetadata)
var allExterns = runtime.Externs
