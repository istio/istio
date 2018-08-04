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

package coverage

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Binaries with code coverage enabled cannot os.Exit() (logFatal, etc...),
// but instead need to exit properly from the test case.

// Helper is here to capture logFatal, Terminate, and normal program exit.
// Capturing SIGTERM is necessary when running inside a container
// such that we can safely return from the test case.
type Helper struct {
	FatalLog  chan string
	Terminate chan os.Signal
	Exit      chan error
}

// Enabled returns true if the env variable COVERAGE is set and equal to true.
func Enabled() bool {
	return os.Getenv("COVERAGE") == "true"
}

// NewHelper instantiate a Helper, and setup notification for SIGINT and SIGTERM.
func NewHelper() *Helper {
	cov := Helper{
		FatalLog:  make(chan string),
		Terminate: make(chan os.Signal),
		Exit:      make(chan error),
	}
	signal.Notify(cov.Terminate, syscall.SIGINT, syscall.SIGTERM)
	return &cov
}

// LogFatal will log message to stdout and set the LogFatal channel.
func (c *Helper) LogFatal(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	_, _ = fmt.Fprintf(os.Stderr, message+"\n") // #nosec
	c.FatalLog <- message
	// Blocking a second for the Test to gracefully shutdown.
	// Not blocking would keep running unwanted code, since we don't exit.
	time.Sleep(time.Second)
}

// Wait for any of the channel to be set, and exit.
func (c *Helper) Wait() error {
	select {
	case log := <-c.FatalLog:
		return fmt.Errorf(log)
	case err := <-c.Exit:
		return err
	case <-c.Terminate:
		return nil
	}
}
