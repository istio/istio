// Copyright 2018 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package interpreter

import (
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

// evalObserver is a functional interface that accepts an expression id and an observed value.
type evalObserver func(int64, ref.Value)

// decObserveEval records evaluation state into an EvalState object.
func decObserveEval(observer evalObserver) InterpretableDecorator {
	return func(i Interpretable) (Interpretable, error) {
		return &evalWatch{
			inst:     i,
			observer: observer,
		}, nil
	}
}

// decDisableShortcircuits ensures that all branches of an expression will be evaluated, no short-circuiting.
func decDisableShortcircuits() InterpretableDecorator {
	return func(i Interpretable) (Interpretable, error) {
		switch i.(type) {
		case *evalOr:
			or := i.(*evalOr)
			return &evalExhaustiveOr{
				id:  or.id,
				lhs: or.lhs,
				rhs: or.rhs,
			}, nil
		case *evalAnd:
			and := i.(*evalAnd)
			return &evalExhaustiveAnd{
				id:  and.id,
				lhs: and.lhs,
				rhs: and.rhs,
			}, nil
		case *evalConditional:
			cond := i.(*evalConditional)
			return &evalExhaustiveConditional{
				id:     cond.id,
				expr:   cond.expr,
				truthy: cond.truthy,
				falsy:  cond.falsy,
			}, nil
		case *evalFold:
			fold := i.(*evalFold)
			return &evalExhaustiveFold{
				id:        fold.id,
				accu:      fold.accu,
				accuVar:   fold.accuVar,
				iterRange: fold.iterRange,
				iterVar:   fold.iterVar,
				cond:      fold.cond,
				step:      fold.step,
				result:    fold.result,
			}, nil
		}
		return i, nil
	}
}

// decFoldConstants checks whether the arguments the create list and map operations are all
// constant values and constructs a new aggregate value. Future improvements to this method will
// also do the same for typed object creations and functions whose arguments are constant.
func decFoldConstants() InterpretableDecorator {
	return func(i Interpretable) (Interpretable, error) {
		switch i.(type) {
		case *evalList:
			l := i.(*evalList)
			for _, elem := range l.elems {
				_, isConst := elem.(*evalConst)
				if !isConst {
					return i, nil
				}
			}
			val := l.Eval(emptyActivation)
			return &evalConst{
				id:  l.id,
				val: val,
			}, nil
		case *evalMap:
			mp := i.(*evalMap)
			for idx, key := range mp.keys {
				_, isConst := key.(*evalConst)
				if !isConst {
					return i, nil
				}
				_, isConst = mp.vals[idx].(*evalConst)
				if !isConst {
					return i, nil
				}
			}
			val := mp.Eval(emptyActivation)
			return &evalConst{
				id:  mp.id,
				val: val,
			}, nil
		}
		return i, nil
	}
}

// evalWatch is an Interpretable implementation that wraps the execution of a given
// expression so that it may observe the computed value and send it to an observer.
type evalWatch struct {
	inst     Interpretable
	observer evalObserver
}

// ID implements the Interpretable interface method.
func (e *evalWatch) ID() int64 {
	return e.inst.ID()
}

// Eval implements the Interpretable interface method.
func (e *evalWatch) Eval(ctx Activation) ref.Value {
	val := e.inst.Eval(ctx)
	e.observer(e.inst.ID(), val)
	return val
}

// evalExhaustiveOr is just like evalOr, but does not short-circuit argument evaluation.
type evalExhaustiveOr struct {
	id  int64
	lhs Interpretable
	rhs Interpretable
}

// ID implements the Interpretable interface method.
func (or *evalExhaustiveOr) ID() int64 {
	return or.id
}

// Eval implements the Interpretable interface method.
func (or *evalExhaustiveOr) Eval(ctx Activation) ref.Value {
	lVal := or.lhs.Eval(ctx)
	rVal := or.rhs.Eval(ctx)
	lBool, lok := lVal.(types.Bool)
	if lok && lBool == types.True {
		return types.True
	}
	rBool, rok := rVal.(types.Bool)
	if rok && rBool == types.True {
		return types.True
	}
	if lok && rok {
		return types.False
	}
	if types.IsUnknown(lVal) {
		return lVal
	}
	if types.IsUnknown(rVal) {
		return rVal
	}
	return types.ValOrErr(lVal, "no such overload")
}

// evalExhaustiveAnd is just like evalAnd, but does not short-circuit argument evaluation.
type evalExhaustiveAnd struct {
	id  int64
	lhs Interpretable
	rhs Interpretable
}

// ID implements the Interpretable interface method.
func (and *evalExhaustiveAnd) ID() int64 {
	return and.id
}

// Eval implements the Interpretable interface method.
func (and *evalExhaustiveAnd) Eval(ctx Activation) ref.Value {
	lVal := and.lhs.Eval(ctx)
	rVal := and.rhs.Eval(ctx)
	lBool, lok := lVal.(types.Bool)
	if lok && lBool == types.False {
		return types.False
	}
	rBool, rok := rVal.(types.Bool)
	if rok && rBool == types.False {
		return types.False
	}
	if lok && rok {
		return types.True
	}
	if types.IsUnknown(lVal) {
		return lVal
	}
	if types.IsUnknown(rVal) {
		return rVal
	}
	return types.ValOrErr(lVal, "no such overload")
}

// evalExhaustiveConditional is like evalConditional, but does not short-circuit argument
// evaluation.
type evalExhaustiveConditional struct {
	id     int64
	expr   Interpretable
	truthy Interpretable
	falsy  Interpretable
}

// ID implements the Interpretable interface method.
func (cond *evalExhaustiveConditional) ID() int64 {
	return cond.id
}

// Eval implements the Interpretable interface method.
func (cond *evalExhaustiveConditional) Eval(ctx Activation) ref.Value {
	cVal := cond.expr.Eval(ctx)
	tVal := cond.truthy.Eval(ctx)
	fVal := cond.falsy.Eval(ctx)
	cBool, ok := cVal.(types.Bool)
	if !ok {
		return types.ValOrErr(cVal, "no such overload")
	}
	if cBool {
		return tVal
	}
	return fVal
}

// evalExhaustiveFold is like evalFold, but does not short-circuit argument evaluation.
type evalExhaustiveFold struct {
	id        int64
	accuVar   string
	iterVar   string
	iterRange Interpretable
	accu      Interpretable
	cond      Interpretable
	step      Interpretable
	result    Interpretable
}

// ID implements the Interpretable interface method.
func (fold *evalExhaustiveFold) ID() int64 {
	return fold.id
}

// Eval implements the Interpretable interface method.
func (fold *evalExhaustiveFold) Eval(ctx Activation) ref.Value {
	foldRange := fold.iterRange.Eval(ctx)
	if !foldRange.Type().HasTrait(traits.IterableType) {
		return types.ValOrErr(foldRange, "got '%T', expected iterable type", foldRange)
	}
	// Configure the fold activation with the accumulator initial value.
	accuCtx := varActivationPool.Get().(*varActivation)
	accuCtx.parent = ctx
	accuCtx.name = fold.accuVar
	accuCtx.val = fold.accu.Eval(ctx)
	iterCtx := varActivationPool.Get().(*varActivation)
	iterCtx.parent = accuCtx
	iterCtx.name = fold.iterVar
	it := foldRange.(traits.Iterable).Iterator()
	for it.HasNext() == types.True {
		// Modify the iter var in the fold activation.
		iterCtx.val = it.Next()

		// Evaluate the condition, but don't terminate the loop as this is exhaustive eval!
		fold.cond.Eval(iterCtx)

		// Evalute the evaluation step into accu var.
		accuCtx.val = fold.step.Eval(iterCtx)
	}
	// Compute the result.
	res := fold.result.Eval(accuCtx)
	varActivationPool.Put(iterCtx)
	varActivationPool.Put(accuCtx)
	return res
}

var (
	emptyActivation = NewActivation(map[string]interface{}{})
)
