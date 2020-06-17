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

package interpreter

import (
	"bytes"
	"fmt"

	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/il"
	"istio.io/istio/mixer/pkg/il/text"
)

// Stepper executes a program using single steps. It is useeful for debugging through an executing
// program. The stepper will capture the state of the interpreter after every execution step and
// stop execution, allowing the developers to inspect the state of the interpreter.
type Stepper struct {
	i *Interpreter

	registers [registerCount]uint32
	opstack   []uint32
	frames    []stackFrame
	heap      []interface{}
	sp        uint32
	ip        uint32
	fp        uint32
	hp        uint32

	fn  *il.Function
	bag attribute.Bag

	result *Result
	err    error

	program   *il.Program
	completed bool
}

// NewStepper returns a new stepper instance that executes the given program and externs.
func NewStepper(p *il.Program, es map[string]Extern) *Stepper {

	s := &Stepper{
		program:   p,
		sp:        0,
		ip:        0,
		fp:        0,
		opstack:   make([]uint32, opStackSize),
		frames:    make([]stackFrame, callStackSize),
		heap:      make([]interface{}, heapSize),
		hp:        0,
		completed: false,
	}
	i := newIntr(p, es, s)

	s.i = i
	return s
}

// Begin starts stepping into the evaluation of the function, identified by fnName.
func (s *Stepper) Begin(fnName string, bag attribute.Bag) error {
	fn := s.i.program.Functions.Get(fnName)
	if fn == nil {
		return fmt.Errorf("function not found: '%s'", fnName)
	}

	s.fn = fn
	s.bag = bag
	s.ip = fn.Address

	return nil
}

// Step executes the next available instruction. Returns true if the program can still
// execute.
func (s *Stepper) Step() bool {
	if s.completed {
		return false
	}

	r, err := s.i.run(s.i.stepper.fn, s.i.stepper.bag, true)

	if s.completed {
		s.result = &r
		s.err = err
	}

	return !s.completed
}

// Done indicates that the stepper is done, and execution is completed.
func (s *Stepper) Done() bool {
	return s.completed
}

// Error returns any error that was accumulated as the outcome of execution.
func (s *Stepper) Error() error {
	return s.err
}

// Result returns the accumulated result of execution.
func (s *Stepper) Result() Result {
	return *s.result
}

// String dumps the current state of the interpreter in a human-readable form.
func (s *Stepper) String() string {
	b := &bytes.Buffer{}
	b.WriteString("\n")
	fmt.Fprintf(b, "sp = %d\n", s.sp)
	fmt.Fprintf(b, "ip = %d\n", s.ip)
	fmt.Fprintf(b, "fp = %d\n", s.fp)
	for i, r := range s.registers {
		fmt.Fprintf(b, "r%d = %d  ", i, r)
	}
	b.WriteString("\n")

	b.WriteString("stack:  [ ")
	for i := 0; i < int(s.sp); i++ {
		fmt.Fprintf(b, "%d ", s.opstack[i])
	}
	b.WriteString("]\n")

	b.WriteString("frames: [ ")
	for i := 0; i < int(s.fp); i++ {
		fmt.Fprintf(b, "%v ", s.frames[i])
	}
	b.WriteString("]\n")

	b.WriteString("heap:   [ ")
	for i := 0; i < int(s.hp); i++ {
		fmt.Fprintf(b, "%v ", s.heap[i])
	}
	b.WriteString("]\n\n")

	b.WriteString("code:   [\n")
	text.WriteFn(b, s.program.ByteCode(), s.fn, s.program.Strings(), s.ip)
	b.WriteString("]\n\n")

	return b.String()
}
