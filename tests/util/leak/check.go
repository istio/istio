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

// leak checks for goroutine leaks in tests
// This is (heavily) inspired by https://github.com/grpc/grpc-go/blob/master/internal/leakcheck/leakcheck.go
// and https://github.com/fortytw2/leaktest
package leak

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/atomic"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
)

var goroutinesToIgnore = []string{
	// "global" goroutines we always initialize. Maybe we shouldn't always initialize these, but for now every
	// test fails with these
	"k8s.io/klog/v2.(*loggingT).flushDaemon",      // k8s logging
	"go.opencensus.io/stats/view.(*worker).start", // metrics runs on init. We are *almost* off opencensus, but transitively import it.

	// goroutines for test
	"testing.Main(",
	"testing.tRunner(",
	"testing.(*M).",

	// go runtime
	"runtime.goexit",
	"created by runtime.gc",
	"runtime.MHeap_Scavenger",
	"signal.signal_recv",
	"sigterm.handler",
	"runtime_mcall",

	// created by leak checker
	"created by runtime/trace.Start",
	"interestingGoroutines",

	// This is not technically required. However, its a loop that is outside our control that runs every 500ms
	// so we skip it to avoid delayed tests
	"workqueue.(*Type).updateUnfinishedWorkLoop",
}

// TestingM is the minimal subset of testing.M that we use.
type TestingM interface {
	Run() int
}

type checkOptions struct {
	allowedLeaks int
}

type CheckOption func(c *checkOptions)

type TestingTB interface {
	Cleanup(func())
	Errorf(format string, args ...any)
}

var gracePeriod = time.Second * 5

func WithAllowedLeaks(n int) CheckOption {
	return func(c *checkOptions) {
		c.allowedLeaks = n
	}
}

func check(filter func(in []*goroutine) []*goroutine, options ...CheckOption) error {
	checkOpts := &checkOptions{
		allowedLeaks: 0, // Default to no leaks
	}
	for _, opt := range options {
		opt(checkOpts)
	}
	// Loop, waiting for goroutines to shut down.
	// Wait up to timeout, but finish as quickly as possible.
	// The timeout here is not super sensitive, since if we hit this we will fail; a happy case will finish quickly
	deadline := time.Now().Add(gracePeriod)
	var leaked []*goroutine
	var err error
	delay := time.Duration(0)
	for time.Now().Before(deadline) {
		leaked, err = interestingGoroutines()
		if err != nil {
			return fmt.Errorf("failed to fetch post-test goroutines: %v", err)
		}
		if filter != nil {
			leaked = filter(leaked)
		}
		if len(leaked) == 0 {
			return nil // No leaks, we are done
		}
		if len(leaked) <= checkOpts.allowedLeaks {
			fmt.Printf("leaked %d goroutines, but allowed %d; passing\n", len(leaked), checkOpts.allowedLeaks)
			return nil
		}
		time.Sleep(delay)
		delay += time.Millisecond * 10
	}
	errString := strings.Builder{}
	for _, g := range leaked {
		errString.WriteString(fmt.Sprintf("Leaked goroutine: %v\n", g.stack))
	}
	return errors.New(errString.String())
}

// Check adds a check to a test to ensure there are no leaked goroutines
// To use, simply call leak.Check(t) at the start of a test; Do not call it in defer.
// It is recommended to call this as the first step, as Cleanup is called in LIFO order; this ensures any
// Cleanup's called in the test happen first.
// Any existing goroutines before the test starts are filtered out. This ensures a single test failing doesn't
// cause all future tests to fail. However, it is still possible another test influences the result when t.Parallel is used.
// Where possible, CheckMain is preferred.
func Check(t TestingTB, options ...CheckOption) {
	existingRaw, err := interestingGoroutines()
	if err != nil {
		t.Errorf("failed to fetch pre-test goroutines: %v", err)
		return
	}
	existing := map[uint64]struct{}{}
	for _, g := range existingRaw {
		existing[g.id] = struct{}{}
	}
	filter := func(in []*goroutine) []*goroutine {
		res := make([]*goroutine, 0, len(in))
		for _, i := range in {
			if _, f := existing[i.id]; !f {
				// This was not in the goroutines list when the test started
				res = append(res, i)
			}
		}
		return res
	}
	t.Cleanup(func() {
		if err := check(filter, options...); err != nil {
			t.Errorf("goroutine leak: %v", err)
		}
	})
}

// CheckMain asserts that no goroutines are leaked after a test package exits.
// This can be used with the following code:
//
//	func TestMain(m *testing.M) {
//	    leak.CheckMain(m)
//	}
//
// Failures here are scoped to the package, not a specific test. To determine the source of the failure,
// you can use the tool `go test -exec $PWD/tools/go-ordered-test ./my/package`. This runs each test individually.
// If there are some tests that are leaky, you the Check method can be used on individual tests.
func CheckMain(m TestingM) {
	exitCode := m.Run()

	if exitCode == 0 {
		if err := check(nil); err != nil {
			log.Errorf("fatal: %v", err)
			exitCode = 1
		}
	}

	os.Exit(exitCode)
}

// MustGarbageCollect asserts that an object was garbage collected by the end of the test.
// The input must be a pointer to an object.
func MustGarbageCollect(tb test.Failer, i any) {
	tb.Helper()
	collected := atomic.NewBool(false)
	runtime.SetFinalizer(i, func(x any) {
		collected.Store(true)
	})
	tb.Cleanup(func() {
		retry.UntilOrFail(tb, func() bool {
			// Trigger GC explicitly, otherwise we may need to wait a long time for it to run
			runtime.GC()
			return collected.Load()
		}, retry.Timeout(time.Second*5), retry.Message("object was not garbage collected"))
	})
}

type goroutine struct {
	id    uint64
	stack string
}

type goroutineByID []*goroutine

func (g goroutineByID) Len() int           { return len(g) }
func (g goroutineByID) Less(i, j int) bool { return g[i].id < g[j].id }
func (g goroutineByID) Swap(i, j int)      { g[i], g[j] = g[j], g[i] }

func interestingGoroutine(g string) (*goroutine, error) {
	sl := strings.SplitN(g, "\n", 2)
	if len(sl) != 2 {
		return nil, fmt.Errorf("error parsing stack: %q", g)
	}
	stack := strings.TrimSpace(sl[1])
	if strings.HasPrefix(stack, "testing.RunTests") {
		return nil, nil
	}

	for _, s := range goroutinesToIgnore {
		if strings.Contains(stack, s) {
			return nil, nil
		}
	}

	// Parse the goroutine's ID from the header line.
	h := strings.SplitN(sl[0], " ", 3)
	if len(h) < 3 {
		return nil, fmt.Errorf("error parsing stack header: %q", sl[0])
	}
	id, err := strconv.ParseUint(h[1], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("error parsing goroutine id: %s", err)
	}

	return &goroutine{id: id, stack: strings.TrimSpace(g)}, nil
}

// interestingGoroutines returns all goroutines we care about for the purpose
// of leak checking. It excludes testing or runtime ones.
func interestingGoroutines() ([]*goroutine, error) {
	buf := make([]byte, 2<<20)
	buf = buf[:runtime.Stack(buf, true)]
	var gs []*goroutine
	for _, g := range strings.Split(string(buf), "\n\n") {
		gr, err := interestingGoroutine(g)
		if err != nil {
			return nil, err
		} else if gr == nil {
			continue
		}
		gs = append(gs, gr)
	}
	sort.Sort(goroutineByID(gs))
	return gs, nil
}
