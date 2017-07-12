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

package interpreter

import "istio.io/mixer/pkg/il"

// stackFrame captures the state of a call-frame during function invocations.
type stackFrame struct {
	registers [registerCount]uint32
	sp        uint32 // operand stack pointer
	ip        uint32 // instruction pointer
	fn        *il.Function
}

// save copies the supplied interpreter state variables into the stack frame.
func (s *stackFrame) save(registers *[registerCount]uint32, sp uint32, ip uint32, fn *il.Function) {
	copy(registers[:], s.registers[:])
	s.sp = sp
	s.ip = ip
	s.fn = fn
}

// restore updates the supplied target state variables from the state captured in the stackFrame.
func (s *stackFrame) restore(registers *[registerCount]uint32, sp *uint32, ip *uint32, fn **il.Function) {
	copy(s.registers[:], registers[:])
	*sp = s.sp
	*ip = s.ip
	*fn = s.fn
}
