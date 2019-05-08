// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package topdown

import (
	"regexp"
	"sync"

	"github.com/yashtewari/glob-intersection"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown/builtins"
)

var regexpCacheLock = sync.Mutex{}
var regexpCache map[string]*regexp.Regexp

func builtinRegexMatch(a, b ast.Value) (ast.Value, error) {
	s1, err := builtins.StringOperand(a, 1)
	if err != nil {
		return nil, err
	}
	s2, err := builtins.StringOperand(b, 2)
	if err != nil {
		return nil, err
	}
	re, err := getRegexp(string(s1))
	if err != nil {
		return nil, err
	}
	return ast.Boolean(re.Match([]byte(s2))), nil
}

func getRegexp(pat string) (*regexp.Regexp, error) {
	regexpCacheLock.Lock()
	defer regexpCacheLock.Unlock()
	re, ok := regexpCache[pat]
	if !ok {
		var err error
		re, err = regexp.Compile(string(pat))
		if err != nil {
			return nil, err
		}
		regexpCache[pat] = re
	}
	return re, nil
}

func builtinGlobsMatch(a, b ast.Value) (ast.Value, error) {
	s1, err := builtins.StringOperand(a, 1)
	if err != nil {
		return nil, err
	}
	s2, err := builtins.StringOperand(b, 2)
	if err != nil {
		return nil, err
	}
	ne, err := gintersect.NonEmpty(string(s1), string(s2))
	if err != nil {
		return nil, err
	}
	return ast.Boolean(ne), nil
}

func init() {
	regexpCache = map[string]*regexp.Regexp{}
	RegisterFunctionalBuiltin2(ast.RegexMatch.Name, builtinRegexMatch)
	RegisterFunctionalBuiltin2(ast.GlobsMatch.Name, builtinGlobsMatch)
}
