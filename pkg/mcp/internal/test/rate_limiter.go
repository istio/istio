// Copyright Istio Authors
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
	"context"

	"istio.io/istio/pkg/mcp/rate"
)

type FakeRateLimiter struct {
	WaitErr chan error
	WaitCh  chan context.Context
}

func NewFakeRateLimiter() *FakeRateLimiter {
	return &FakeRateLimiter{
		WaitErr: make(chan error, 10),
		WaitCh:  make(chan context.Context, 10),
	}
}

func (f *FakeRateLimiter) Wait(ctx context.Context) error {
	f.WaitCh <- ctx
	return <-f.WaitErr
}

type FakePerConnLimiter struct {
	fakeLimiter *FakeRateLimiter
	CreateCh    chan struct{}
	WaitCh      chan context.Context
	ErrCh       chan error
}

func NewFakePerConnLimiter() *FakePerConnLimiter {
	waitErr := make(chan error)
	fakeLimiter := NewFakeRateLimiter()
	fakeLimiter.WaitErr = waitErr

	f := &FakePerConnLimiter{
		fakeLimiter: fakeLimiter,
		CreateCh:    make(chan struct{}, 10),
		WaitCh:      fakeLimiter.WaitCh,
		ErrCh:       waitErr,
	}
	return f
}

func (f *FakePerConnLimiter) Create() rate.Limit {
	f.CreateCh <- struct{}{}
	return f.fakeLimiter
}
