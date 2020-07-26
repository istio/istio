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
	"fmt"
	"sync"
	"testing"
	"time"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/pkg/listwatch"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// Env is an adapter environment that defers to the testing context t. Tracks all messages logged so they can be tested against.
type Env struct {
	done chan struct{} // A channel to notify async work done
	lock sync.Mutex    // guards logs
	logs []string
}

// NewEnv returns an adapter environment that redirects logging output to the given testing context.
func NewEnv(_ *testing.T) *Env {
	return &Env{make(chan struct{}), sync.Mutex{}, make([]string, 0)}
}

// Logger returns a logger that writes to testing.T.Log
func (e *Env) Logger() adapter.Logger {
	return e
}

// ScheduleWork runs the given function asynchronously.
func (e *Env) ScheduleWork(fn adapter.WorkFunc) {
	go func() {
		fn()
		e.done <- struct{}{}
	}()
}

// ScheduleDaemon runs the given function asynchronously.
func (e *Env) ScheduleDaemon(fn adapter.DaemonFunc) {
	go func() {
		fn()
		e.done <- struct{}{}
	}()
}

// Infof logs the provided message.
func (e *Env) Infof(format string, args ...interface{}) {
	e.log(format, args...)
}

// Warningf logs the provided message.
func (e *Env) Warningf(format string, args ...interface{}) {
	e.log(format, args...)
}

// Errorf logs the provided message and returns it as an error.
func (e *Env) Errorf(format string, args ...interface{}) error {
	s := e.log(format, args...)
	return fmt.Errorf(s)
}

// Debugf logs the provided message.
func (e *Env) Debugf(format string, args ...interface{}) {
	e.log(format, args...)
}

// InfoEnabled logs the provided message.
func (e *Env) InfoEnabled() bool {
	return true
}

// WarnEnabled logs the provided message.
func (e *Env) WarnEnabled() bool {
	return true
}

// ErrorEnabled logs the provided message.
func (e *Env) ErrorEnabled() bool {
	return true
}

// DebugEnabled logs the provided message.
func (e *Env) DebugEnabled() bool {
	return true
}

// GetLogs returns a snapshot of all logs that've been written to this environment
func (e *Env) GetLogs() []string {
	e.lock.Lock()
	snapshot := make([]string, len(e.logs))
	_ = copy(snapshot, e.logs)
	e.lock.Unlock()
	return snapshot
}

func (e *Env) log(format string, args ...interface{}) string {
	l := fmt.Sprintf(format, args...)

	e.lock.Lock()
	e.logs = append(e.logs, l)
	e.lock.Unlock()

	return l
}

// GetDoneChan returns the channel that returns notification when the async work is done.
func (e *Env) GetDoneChan() chan struct{} {
	return e.done
}

func (e *Env) NewInformer(
	clientset kubernetes.Interface,
	objType runtime.Object,
	duration time.Duration,
	listerWatcher func(namespace string) cache.ListerWatcher,
	indexers cache.Indexers) cache.SharedIndexInformer {

	mlw := listwatch.MultiNamespaceListerWatcher([]string{""}, listerWatcher)
	return cache.NewSharedIndexInformer(mlw, objType, duration, indexers)
}
