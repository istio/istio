// Copyright 2017 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package topdown

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown/builtins"
)

type nowKeyID string

var nowKey = nowKeyID("time.now_ns")

func builtinTimeNowNanos(bctx BuiltinContext, _ []*ast.Term, iter func(*ast.Term) error) error {

	exist, ok := bctx.Cache.Get(nowKey)
	var now *ast.Term

	if !ok {
		curr := time.Now()
		now = ast.NewTerm(ast.Number(int64ToJSONNumber(curr.UnixNano())))
		bctx.Cache.Put(nowKey, now)
	} else {
		now = exist.(*ast.Term)
	}

	return iter(now)
}

func builtinTimeParseNanos(a, b ast.Value) (ast.Value, error) {

	format, err := builtins.StringOperand(a, 1)
	if err != nil {
		return nil, err
	}

	value, err := builtins.StringOperand(b, 2)
	if err != nil {
		return nil, err
	}

	result, err := time.Parse(string(format), string(value))
	if err != nil {
		return nil, err
	}

	return ast.Number(int64ToJSONNumber(result.UnixNano())), nil
}

func builtinTimeParseRFC3339Nanos(a ast.Value) (ast.Value, error) {

	value, err := builtins.StringOperand(a, 1)
	if err != nil {
		return nil, err
	}

	result, err := time.Parse(time.RFC3339, string(value))
	if err != nil {
		return nil, err
	}

	return ast.Number(int64ToJSONNumber(result.UnixNano())), nil
}
func builtinParseDurationNanos(a ast.Value) (ast.Value, error) {

	duration, err := builtins.StringOperand(a, 1)
	if err != nil {
		return nil, err
	}
	value, err := time.ParseDuration(string(duration))
	if err != nil {
		return nil, err
	}
	return ast.Number(int64ToJSONNumber(int64(value))), nil
}

func builtinDate(a ast.Value) (ast.Value, error) {

	value, err := builtins.NumberOperand(a, 1)
	if err != nil {
		return nil, err
	}

	f := builtins.NumberToFloat(value)
	i64, acc := f.Int64()
	if acc != big.Exact {
		return nil, fmt.Errorf("timestamp too big")
	}

	t := time.Unix(0, i64).UTC()
	year, month, day := t.Date()
	result := ast.Array{ast.IntNumberTerm(year), ast.IntNumberTerm(int(month)), ast.IntNumberTerm(day)}
	return result, nil
}

func builtinClock(a ast.Value) (ast.Value, error) {

	value, err := builtins.NumberOperand(a, 1)
	if err != nil {
		return nil, err
	}

	f := builtins.NumberToFloat(value)
	i64, acc := f.Int64()
	if acc != big.Exact {
		return nil, fmt.Errorf("timestamp too big")
	}

	t := time.Unix(0, i64).UTC()
	hour, minute, second := t.Clock()
	result := ast.Array{ast.IntNumberTerm(hour), ast.IntNumberTerm(minute), ast.IntNumberTerm(second)}
	return result, nil
}

func int64ToJSONNumber(i int64) json.Number {
	return json.Number(strconv.FormatInt(i, 10))
}

func init() {
	RegisterBuiltinFunc(ast.NowNanos.Name, builtinTimeNowNanos)
	RegisterFunctionalBuiltin1(ast.ParseRFC3339Nanos.Name, builtinTimeParseRFC3339Nanos)
	RegisterFunctionalBuiltin2(ast.ParseNanos.Name, builtinTimeParseNanos)
	RegisterFunctionalBuiltin1(ast.ParseDurationNanos.Name, builtinParseDurationNanos)
	RegisterFunctionalBuiltin1(ast.Date.Name, builtinDate)
	RegisterFunctionalBuiltin1(ast.Clock.Name, builtinClock)
}
