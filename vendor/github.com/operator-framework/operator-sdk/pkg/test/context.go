// Copyright 2018 The Operator-SDK Authors
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

package test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
)

type TestCtx struct {
	id         string
	cleanupFns []cleanupFn
	namespace  string
	t          *testing.T
}

type CleanupOptions struct {
	TestContext   *TestCtx
	Timeout       time.Duration
	RetryInterval time.Duration
}

type cleanupFn func() error

func NewTestCtx(t *testing.T) *TestCtx {
	var prefix string
	if t != nil {
		// TestCtx is used among others for namespace names where '/' is forbidden
		prefix = strings.TrimPrefix(
			strings.Replace(
				strings.ToLower(t.Name()),
				"/",
				"-",
				-1,
			),
			"test",
		)
	} else {
		prefix = "main"
	}

	id := prefix + "-" + strconv.FormatInt(time.Now().Unix(), 10)
	return &TestCtx{
		id: id,
		t:  t,
	}
}

func (ctx *TestCtx) GetID() string {
	return ctx.id
}

func (ctx *TestCtx) Cleanup() {
	failed := false
	for i := len(ctx.cleanupFns) - 1; i >= 0; i-- {
		err := ctx.cleanupFns[i]()
		if err != nil {
			failed = true
			if ctx.t != nil {
				ctx.t.Errorf("A cleanup function failed with error: (%v)\n", err)
			} else {
				log.Errorf("A cleanup function failed with error: (%v)", err)
			}
		}
	}
	if ctx.t == nil && failed {
		log.Fatal("A cleanup function failed")
	}
}

func (ctx *TestCtx) AddCleanupFn(fn cleanupFn) {
	ctx.cleanupFns = append(ctx.cleanupFns, fn)
}
