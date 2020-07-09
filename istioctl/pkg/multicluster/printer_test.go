// Copyright Istio Authors.
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

package multicluster

import (
	"bytes"
	"fmt"
	"io"
)

var _ Printer = &fakePrinter{}

type fakePrinter struct {
	Out bytes.Buffer
	Err bytes.Buffer
}

func newFakePrinter() *fakePrinter {
	return &fakePrinter{}
}

func (f fakePrinter) Stdout() io.Writer {
	return &f.Out
}

func (f fakePrinter) Stderr() io.Writer {
	return &f.Err
}

func (f fakePrinter) Printf(format string, a ...interface{}) {
	_, _ = fmt.Fprintf(f.Stdout(), format, a...)
}

func (f fakePrinter) Errorf(format string, a ...interface{}) {
	_, _ = fmt.Fprintf(f.Stderr(), format, a...)
}
