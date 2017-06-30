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

package text

import (
	"fmt"
	"strconv"
	"strings"

	"istio.io/mixer/pkg/il"
)

// ReadText parses the given il assembly text and converts it into a Program.
func ReadText(text string) (*il.Program, error) {
	p := il.NewProgram()
	if err := MergeText(text, p); err != nil {
		return nil, err
	}
	return p, nil
}

// MergeText parses the given il assembly text and merges its contents into the Program.
func MergeText(text string, program *il.Program) error {
	return parse(text, program)
}

// parser contains the parsing state of il assemly text.
type parser struct {
	scanner *scanner
	error   error

	program *il.Program
}

// parse the given text as il assembly and merge its contents into the given program.
func parse(text string, program *il.Program) error {
	p := &parser{
		scanner: newScanner(text),
		error:   nil,
		program: program,
	}

	for !p.scanner.end() {
		if !p.scanner.next() {
			break
		}
		if p.scanner.token == tkError {
			p.fail("Parse error.")
			break
		}
		if !p.parseFunctionDef() {
			break
		}
	}

	if p.failed() {
		return p.error
	}

	return nil
}

func (p *parser) skipNewlines() {
	for p.scanner.token == tkNewLine && p.scanner.next() {
	}
}

func (p *parser) parseFunctionDef() bool {
	p.skipNewlines()

	if p.scanner.end() {
		return false
	}

	var f bool
	var i string
	if i, f = p.identifierOrFail(); !f {
		return false
	}
	if i != "fn" {
		p.fail("Expected 'fn'.")
		return false
	}

	if !p.nextToken(tkIdentifier) {
		return false
	}

	name, _ := p.scanner.asIdentifier()

	if !p.nextToken(tkOpenParen) {
		return false
	}

	var pTypes = []il.Type{}
	for {
		if !p.nextOrFail() {
			return false
		}
		if p.scanner.token != tkIdentifier {
			break
		}

		n, _ := p.scanner.asIdentifier()
		var pType il.Type
		if pType, f = il.GetType(n); !f {
			p.fail("Unrecognized parameter type: '%s'", n)
			return false
		}

		pTypes = append(pTypes, pType)
	}

	if !p.currentToken(tkCloseParen) {
		return false
	}

	if !p.nextToken(tkIdentifier) {
		return false
	}

	i, _ = p.scanner.asIdentifier()
	var rType il.Type
	if rType, f = il.GetType(i); !f {
		p.fail("Unrecognized return type: '%s'", i)
		return false
	}

	if !p.nextToken(tkNewLine) {
		return false
	}

	var body []uint32
	if body, f = p.parseFunctionBody(); !f {
		return false
	}

	if err := p.program.AddFunction(name, pTypes, rType, body); err != nil {
		p.failErr(err)
		return false
	}

	return true
}

func (p *parser) parseFunctionBody() ([]uint32, bool) {
	// labels keeps the address of labels that are encountered. The addresses are local to function bytecode.
	labels := make(map[string]uint32)
	// labelRefLocs holds the location of a reference to a label.
	labelRefLocs := make(map[int]location)
	// fixup holds the byte-code locations that needs to be fixed up, once the address of a label is obtained.
	fixups := make(map[int]string)

	body := make([]uint32, 0)

Loop:
	for {
		p.skipNewlines()

		var i string
		switch p.scanner.token {
		case tkLabel:
			l, _ := p.scanner.asLabel()
			labels[l] = uint32(len(body))
			if !p.nextOrFail() {
				goto FAILED
			}
			continue Loop

		case tkIdentifier:
			i, _ = p.scanner.asIdentifier()
			if i == "end" {
				break Loop
			}

		default:
			p.failUnexpectedInput()
			break Loop
		}

		op, f := il.GetOpcode(i)
		if !f {
			p.fail("unrecognized opcode: '%s'", i)
			break Loop
		}
		body = append(body, uint32(op))

		for _, arg := range op.Args() {

			if !p.nextOrFail() {
				goto FAILED
			}

			switch arg {
			case il.OpcodeArgString:
				if i, f = p.scanner.asStringLiteral(); !f {
					p.failUnexpectedInput()
					goto FAILED
				}
				id := p.program.Strings().GetID(i)
				body = append(body, id)

			case il.OpcodeArgFunction:

				if i, f = p.scanner.asIdentifier(); !f {
					p.failUnexpectedInput()
					goto FAILED
				}
				id := p.program.Strings().GetID(i)
				body = append(body, id)

			case il.OpcodeArgInt:
				var i64 int64
				if i64, f = p.scanner.asIntegerLiteral(); !f {
					p.failUnexpectedInput()
					goto FAILED
				}
				t1, t2 := il.IntegerToByteCode(i64)
				body = append(body, t1)
				body = append(body, t2)

			case il.OpcodeArgDouble:
				var i1, i2 uint32
				var f64 float64
				var i64 int64
				if f64, f = p.scanner.asFloatLiteral(); f {
					i1, i2 = il.DoubleToByteCode(f64)
				} else if i64, f = p.scanner.asIntegerLiteral(); f {
					i1, i2 = il.DoubleToByteCode(float64(i64))
				} else {
					p.failUnexpectedInput()
					goto FAILED
				}
				body = append(body, i1)
				body = append(body, i2)

			case il.OpcodeArgBool:
				if i, f = p.scanner.asIdentifier(); !f {
					p.failUnexpectedInput()
					goto FAILED
				}
				switch i {
				case "true":
					body = append(body, uint32(1))
				case "false":
					body = append(body, uint32(0))
				default:
					p.failUnexpectedInput()
					goto FAILED
				}

			case il.OpcodeArgAddress:
				if i, f = p.scanner.asIdentifier(); !f {
					p.failUnexpectedInput()
					goto FAILED
				}
				fixups[len(body)] = i
				labelRefLocs[len(body)] = p.scanner.lBegin
				body = append(body, 0)

			case il.OpcodeArgRegister:
				if i, f = p.scanner.asIdentifier(); !f {
					p.failUnexpectedInput()
					goto FAILED
				}
				if !strings.HasPrefix(i, "r") {
					p.fail("Invalid register name: '%s'", i)
					goto FAILED
				}
				id, err := strconv.Atoi(i[1:])
				if err != nil {
					p.fail("Invalid register name: '%s'", i)
					goto FAILED
				}
				body = append(body, uint32(id))

			default:
				p.fail("Unknown op code arg type.")
				goto FAILED
			}
		}

		if !p.nextToken(tkNewLine) {
			goto FAILED
		}
	}

	if p.failed() {
		goto FAILED
	}

	for index, label := range fixups {
		ival, exists := labels[label]
		if !exists {
			p.failLoc(labelRefLocs[index], "Label not found: %s", label)
			return body, false
		}
		body[index] = ival
	}

	return body, true

FAILED:
	return body, false
}

func (p *parser) failUnexpectedInput() {
	p.fail("unexpected input: '%s'", p.scanner.rawText())
}

func (p *parser) fail(format string, args ...interface{}) {
	p.failLoc(p.scanner.lBegin, format, args...)
}

func (p *parser) failLoc(l location, format string, args ...interface{}) {
	p.failErr(fmt.Errorf("%s @%v", fmt.Sprintf(format, args...), l))
}

func (p *parser) failErr(err error) {
	p.error = err
}

func (p *parser) failed() bool {
	return p.error != nil
}

func (p *parser) nextOrFail() bool {
	if !p.scanner.next() || p.scanner.token == tkNone {
		p.fail("unexpected end of file.")
		return false
	}
	if p.scanner.token == tkError {
		p.fail("Parse error.")
		return false
	}
	return true
}

func (p *parser) nextToken(t token) bool {
	if !p.nextOrFail() {
		return false
	}
	return p.currentToken(t)
}

func (p *parser) currentToken(t token) bool {
	if p.scanner.end() {
		p.fail("unexpected end of file encountered")
		return false
	}
	if p.scanner.token != t {
		p.failUnexpectedInput()
		return false
	}
	return true
}

func (p *parser) identifierOrFail() (string, bool) {
	if p.scanner.token != tkIdentifier {
		p.failUnexpectedInput()
		return "", false
	}

	return p.scanner.asIdentifier()
}
