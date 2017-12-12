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

package perf

import (
	"os"
	"os/exec"
	"testing"
)

// benchmark is an interface that behaves similar to testing.B. It allows tests to mock testing.B.
type benchmark interface {
	name() string
	fatalf(format string, args ...interface{})
	logf(format string, args ...interface{})
	run(name string, fn func(benchmark))
	n() int
}

type testingBWrapper struct {
	b *testing.B
}

var _ benchmark = &testingBWrapper{}

func (b *testingBWrapper) name() string {
	return b.b.Name()
}

func (b *testingBWrapper) fatalf(format string, args ...interface{}) {
	b.b.Fatalf(format, args...)
}

func (b *testingBWrapper) logf(format string, args ...interface{}) {
	b.b.Logf(format, args...)
}

func (b *testingBWrapper) run(name string, fn func(benchmark)) {
	b.b.Run(name, func(bb *testing.B) {
		fn(&testingBWrapper{b: bb})
	})
}

func (b *testingBWrapper) n() int {
	return b.b.N
}

// RunInprocess executes the benchmark by launching the mixer within the same process.
func RunInprocess(b *testing.B, setup *Setup, env *Env) {
	run(&testingBWrapper{b: b}, setup, env, "", false)
}

// RunCoprocess executes the benchmark by launching the Mixer through another process. The process is located
// through an iterative search, starting with the current working directory, and using the executablePathSuffix as the
// search suffix for executable (e.g. "bazel-bin/mixer/test/perf/perfclient/perfclient").
func RunCoprocess(b *testing.B, setup *Setup, env *Env, executablePathSuffix string) {
	run(&testingBWrapper{b: b}, setup, env, executablePathSuffix, true)
}

func run(b benchmark, setup *Setup, env *Env, executablePathSuffix string, coprocess bool) {

	server := &server{}
	if err := server.initialize(setup, env); err != nil {
		b.fatalf("error: %v\n", err)
		return
	}
	defer server.shutdown()

	controller, err := newController()
	if err != nil {
		b.fatalf("controller initialization failed: '%v'", err)
		return
	}
	defer controller.close()

	if coprocess {
		var exeName string
		if exeName, err = locatePerfClientProcess(executablePathSuffix); err != nil {
			b.fatalf("Unable to locate perf mixer")
			return
		}

		b.logf("External process exec name: %s", exeName)
		cmd := exec.Command(exeName, controller.location().Address, controller.location().Path)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err = cmd.Start(); err != nil {
			b.fatalf("Unable to start the remote mixer process")
			return
		}
		defer func() { _ = cmd.Process.Kill() }()
	} else {
		if _, err = NewClientServer(controller.location()); err != nil {
			b.fatalf("agent creation failed: '%v'", err)
			return
		}
	}

	controller.waitForClient()

	if err = controller.initializeClients(server.address(), setup); err != nil {
		b.fatalf("agent initialization failed: '%v'", err)
		return
	}

	// Do a test run first to see if there are any errors.
	if err = controller.runClients(1); err != nil {
		b.fatalf("error during test client run: '%v'", err)
	}

	b.run(b.name(), func(bb benchmark) {
		_ = controller.runClients(bb.n())
	})
	b.logf("completed running test: %s", b.name())

	// Even though we have a deferred close for controller, do it explicitly before leaving control to perform
	// graceful close of clients during teardown.
	controller.close()
}
