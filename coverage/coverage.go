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
	"os"
	"os/signal"
	"fmt"
	"syscall"
)

type Coverage struct {
	FatalLog  chan string
	Terminate chan os.Signal
	Exit      chan error
}

func Enabled() bool {
	return os.Getenv("COVERAGE") == "true"
}

func NewCoverage() *Coverage {
	cov := Coverage{
		FatalLog:  make(chan string),
		Terminate: make(chan os.Signal),
		Exit:      make(chan error),
	}
	signal.Notify(cov.Terminate, syscall.SIGINT, syscall.SIGTERM)
	return &cov
}

func (c *Coverage) LogFatal(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	_, _ = fmt.Fprintf(os.Stderr, message+"\n") // #nosec
	c.FatalLog <- message
}

func (c *Coverage) Wait() error {

	select {
	case log := <-c.FatalLog:
		return fmt.Errorf(log)
	case err := <-c.Exit:
		return err
	case <-c.Terminate:
		return nil
	}

}
