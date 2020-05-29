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

package perf

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	istio_mixer_v1 "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/pkg/attribute"
	mixer "istio.io/istio/mixer/pkg/server"
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

// Run executes the benchmark using specified setup and settings.
func Run(b *testing.B, setup *Setup, settings Settings) {
	switch settings.RunMode {
	case InProcess:
		run(&testingBWrapper{b}, setup, &settings, false)
	case CoProcess:
		run(&testingBWrapper{b}, setup, &settings, true)
	case InProcessBypassGrpc:
		runDispatcherOnly(&testingBWrapper{b}, setup, &settings)
	default:
		b.Fatalf("Unknown run mode: %v", settings.RunMode)
	}
}

func run(b benchmark, setup *Setup, settings *Settings, coprocess bool) {

	server := &server{}
	if err := server.initialize(setup, settings); err != nil {
		b.fatalf("error: %v\n", err)
		return
	}
	defer server.shutdown()

	controller, err := newController()
	if err != nil {
		b.fatalf("controller initialization failed: '%v'", err)
		return
	}
	defer func() {
		_ = controller.close()
	}()

	if coprocess {
		var exeName string
		if exeName, err = locatePerfClientProcess(settings.ExecutablePathSuffix); err != nil {
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
	} else if _, err = NewClientServer(controller.location()); err != nil {
		b.fatalf("agent creation failed: '%v'", err)
		return
	}

	controller.waitForClient()

	if err = controller.initializeClients(server.address(), setup); err != nil {
		b.fatalf("agent initialization failed: '%v'", err)
		return
	}

	// Do a test run first to see if there are any errors.
	if err = controller.runClients(1, time.Duration(0)); err != nil {
		b.fatalf("error during test client run: '%v'", err)
	}

	name := "InProc"
	if coprocess {
		name = "CoProc"
	}
	b.run(name, func(bb benchmark) {
		_ = controller.runClients(bb.n(), time.Duration(0))
	})

	// Even though we have a deferred close for controller, do it explicitly before leaving control to perform
	// graceful close of clients during teardown.
	_ = controller.close()
}

func runDispatcherOnly(b benchmark, setup *Setup, settings *Settings) {
	a, err := initializeArgs(settings, setup)
	if err != nil {
		b.fatalf("error initializing args: '%s'", err.Error())
		return
	}

	s, err := mixer.New(a)
	if err != nil {
		b.fatalf("error creating Mixer server: '%s'", err.Error())
		return
	}
	defer func() {
		_ = s.Close()
	}()

	dispatcher := s.Dispatcher()

	list := attribute.GlobalList()
	globalDict := make(map[string]int32, len(list))
	for i := 0; i < len(list); i++ {
		globalDict[list[i]] = int32(i)
	}

	// there has to be just 1 load for InProcessBypassGrpc case
	if len(setup.Loads) != 1 {
		b.fatalf("for `InProcessBypassGrpc`, load must contain exactly 1 entry")
		return
	}
	requests := setup.Loads[0].createRequestProtos()
	bags := make([]attribute.Bag, len(requests)) // precreate bags to avoid polluting allocation data.
	for i, r := range requests {
		switch req := r.(type) {
		case *istio_mixer_v1.ReportRequest:
			bags[i] = attribute.GetProtoBag(&req.Attributes[0], globalDict, attribute.GlobalList())

		case *istio_mixer_v1.CheckRequest:
			bags[i] = attribute.GetProtoBag(&req.Attributes, globalDict, attribute.GlobalList())

		default:
			b.fatalf("unknown request type: %v", r)
		}
	}

	// Run through once to detect if there are any errors
	for j, r := range requests {
		bag := bags[j]

		switch r.(type) {
		case *istio_mixer_v1.ReportRequest:
			r := dispatcher.GetReporter(context.Background())
			err = r.Report(bag)
			if err == nil {
				err = r.Flush()
			}
			r.Done()

		case *istio_mixer_v1.CheckRequest:
			_, err = dispatcher.Check(context.Background(), bag)
		}

		if err != nil {
			b.fatalf("Error detected during run: %v", err)
		}
	}

	b.run("InProcessBypassGrpc", func(bb benchmark) {
		for i := 0; i < bb.n(); i++ {
			for j, r := range requests {
				bag := bags[j]

				switch r.(type) {
				case *istio_mixer_v1.ReportRequest:
					r := dispatcher.GetReporter(context.Background())
					_ = r.Report(bag)
					_ = r.Flush()
					r.Done()

				case *istio_mixer_v1.CheckRequest:
					_, _ = dispatcher.Check(context.Background(), bag)
				}
			}
		}
	})
}
