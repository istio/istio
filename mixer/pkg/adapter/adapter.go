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

// Package adapter defines the types consumed by adapter implementations to
// interface with Mixer.
package adapter

import (
	"time"

	"github.com/gogo/protobuf/proto"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

type (
	// Config represents a chunk of adapter configuration state
	Config proto.Message

	// WorkFunc represents a function to invoke.
	WorkFunc func()

	// DaemonFunc represents a function to invoke asynchronously to run a long-running background processing loop.
	DaemonFunc func()

	// Env defines the environment in which an adapter executes.
	Env interface {
		// Logger returns the logger for the adapter to use at runtime.
		Logger() Logger

		// ScheduleWork records a function for execution.
		//
		// Under normal circumstances, this method executes the
		// function on a separate goroutine. But when Mixer is
		// running in single-threaded mode, then the function
		// will be invoked synchronously on the same goroutine.
		//
		// Adapters should not spawn 'naked' goroutines, they should
		// use this method or ScheduleDaemon instead.
		ScheduleWork(fn WorkFunc)

		// ScheduleDaemon records a function for background execution.
		// Unlike ScheduleWork, this method guarantees execution on a
		// different goroutine. Use ScheduleDaemon for long-running
		// background operations, whereas ScheduleWork is for bursty
		// one-shot kind of things.
		//
		// Adapters should not spawn 'naked' goroutines, they should
		// use this method or ScheduleWork instead.
		ScheduleDaemon(fn DaemonFunc)

		// Possible other features for Env:
		// Return how much time remains until Mixer considers the adapter call having timed out and kills it
		// Return true/false to indicate this is a 'recovery mode' execution following a prior crash of the adapter
		// ?

		// Create a new informer monitoring the application namespaces
		NewInformer(clientset kubernetes.Interface, objType runtime.Object, duration time.Duration, listerWatcher func(namespace string) cache.ListerWatcher,
			indexers cache.Indexers) cache.SharedIndexInformer
	}

	// Logger defines where adapters should output their log state to.
	//
	// This log is funneled to Mixer which augments it with
	// desirable metadata and then routes it to the right place.
	Logger interface {
		// Infof logs optional information.
		Infof(format string, args ...interface{})

		// Warningf logs suspect situations and recoverable errors
		Warningf(format string, args ...interface{})

		// Errorf logs error conditions.
		// In addition to generating a log record for the error, this also returns
		// an error instance for convenience.
		Errorf(format string, args ...interface{}) error

		// Debugf logs potentially verbose debug-time data
		Debugf(format string, args ...interface{})

		// InfoEnabled returns whether output of messages at the info level is currently enabled.
		InfoEnabled() bool

		// InfoEnabled returns whether output of messages at the warn level is currently enabled.
		WarnEnabled() bool

		// ErrorEnabled returns whether output of messages at the wanr level is currently enabled.
		ErrorEnabled() bool

		// DebugEnabled returns whether output of messages at the debug level is currently enabled.
		DebugEnabled() bool
	}
)
