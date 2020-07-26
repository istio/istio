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

package text

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type token int

const (
	tkNone token = iota
	tkError
	tkIdentifier
	tkStringLiteral
	tkIntegerLiteral
	tkFloatLiteral
	tkNewLine
	tkLabel
	tkOpenParen
	tkCloseParen
)

type scanState int

const (
	scScan scanState = iota
	scBeginComment
	scComment
	scStringLiteral
	scStringLiteralEscape
	scDecimalOrHexOrFloatLiteral
	scDecimalOrFloatLiteral
	scFloatLiteral
	scHexLiteral
	scIdentifierOrLabel
	scEnd
	scError
)

// scanner tokenizes an input string for parsing as IL text.
type scanner struct {
	text     string
	begin    int
	lBegin   location
	current  int
	state    scanState
	lCurrent location
	token    token
}

type location struct {
	line   int
	column int
}

func (l location) String() string {
	return fmt.Sprintf("(L: %d, C: %d)", l.line, l.column)
}

func newScanner(text string) *scanner {
	return &scanner{
		text:     text,
		state:    scScan,
		lBegin:   location{line: 1, column: 1},
		begin:    0,
		current:  0,
		lCurrent: location{line: 1, column: 1},
		token:    tkNone,
	}
}

func (s *scanner) next() bool {
	if s.end() {
		return false
	}

	s.state = scScan
	s.token = tkNone

	done := false
	for _, rn := range s.text[s.current:] {
		done = s.onRune(rn)
		if done {
			break
		}

		s.advance(rn)
	}

	if !done {
		done = s.onRune(0)

		if s.state != scError {
			s.state = scEnd
		}
	}

	if s.state == scError {
		s.token = tkError
		s.begin = s.current
		s.lBegin = s.lCurrent
	}

	return done
}

func (s *scanner) advance(rn rune) {
	if rn == '\n' {
		s.lCurrent.line++
		s.lCurrent.column = 0
	}
	l := utf8.RuneLen(rn)
	s.current += l
	s.lCurrent.column++
}

func (s *scanner) onRune(rn rune) bool {
	switch s.state {
	case scScan:
		s.markBegin()
		switch rn {
		case 0:
		case '\n':
			s.token = tkNewLine
			s.advance(rn)
		case '/':
			s.state = scBeginComment
		case '"':
			s.state = scStringLiteral
		case '(':
			s.token = tkOpenParen
			s.advance(rn)
		case ')':
			s.token = tkCloseParen
			s.advance(rn)
		case '0', '-':
			s.state = scDecimalOrHexOrFloatLiteral
		case '1', '2', '3', '4', '5', '6', '7', '8', '9':
			s.state = scDecimalOrFloatLiteral
		case '.':
			s.state = scFloatLiteral
		default:
			if unicode.IsLetter(rn) {
				s.state = scIdentifierOrLabel
			} else if unicode.IsSpace(rn) {
				break
			} else {
				s.state = scError
			}
		}

	case scBeginComment:
		switch rn {
		case '/':
			s.state = scComment
		default:
			s.state = scError
		}

	case scComment:
		switch rn {
		case '\n':
			s.markBegin()
			s.token = tkNewLine
			s.advance(rn)
		}

	case scStringLiteral:
		switch rn {
		case '\\':
			s.state = scStringLiteralEscape
		case '"':
			s.token = tkStringLiteral
			s.advance(rn)
		case '\n', 0:
			s.state = scError
		}

	case scStringLiteralEscape:
		switch rn {
		case 0, '\n':
			s.state = scError
		default:
			s.state = scStringLiteral
		}

	case scIdentifierOrLabel:
		switch {
		case rn == ':':
			s.token = tkLabel
			s.advance(rn)
		case unicode.IsSpace(rn) || rn == '\n' || rn == '/' || rn == '(' || rn == ')' || rn == 0:
			s.token = tkIdentifier
		case !unicode.IsDigit(rn) && !unicode.IsLetter(rn) && rn != '_':
			s.state = scError
		}

	case scDecimalOrHexOrFloatLiteral:
		switch rn {
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			s.state = scDecimalOrFloatLiteral
		case 'x', 'X':
			s.state = scHexLiteral
		default:
			s.token = tkIntegerLiteral
			if !(unicode.IsSpace(rn) || rn == '/' || rn == 0) {
				s.state = scError
			}
		}

	case scDecimalOrFloatLiteral:
		switch rn {
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':

		case '.':
			s.state = scFloatLiteral
		default:
			s.token = tkIntegerLiteral
			if !(unicode.IsSpace(rn) || rn == '/' || rn == 0) {
				s.state = scError
			}
		}

	case scFloatLiteral:
		switch rn {
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':

		default:
			s.token = tkFloatLiteral
			if !(unicode.IsSpace(rn) || rn == '/' || rn == 0) {
				s.state = scError
			}
		}

	case scHexLiteral:
		switch {
		case unicode.IsDigit(rn):
		case rn >= 'a' && rn <= 'f',
			rn >= 'A' && rn <= 'F':
			break
		default:
			s.token = tkIntegerLiteral
			if !(unicode.IsSpace(rn) || rn == '/' || rn == 0) {
				s.state = scError
			}
		}

	default:
		panic(fmt.Errorf("unknown state: %v", s.state))
	}

	return s.state == scError || s.token != tkNone
}

func (s *scanner) markBegin() {
	s.begin = s.current
	s.lBegin = s.lCurrent
}

func (s *scanner) rawText() string {
	return s.text[s.begin:s.current]
}

func (s *scanner) asIntegerLiteral() (int64, bool) {
	if s.token == tkIntegerLiteral {
		r, err := strconv.ParseInt(s.rawText(), 0, 64)
		if err != nil {
			panic(err)
		}
		return r, true
	}
	return 0, false
}

func (s *scanner) asFloatLiteral() (float64, bool) {
	if s.token == tkFloatLiteral {
		r, err := strconv.ParseFloat(s.rawText(), 64)
		if err != nil {
			panic(err)
		}
		return r, true
	}
	return 0, false
}

func (s *scanner) asIdentifier() (string, bool) {
	if s.token == tkIdentifier {
		return s.rawText(), true
	}
	return "", false
}

func (s *scanner) asLabel() (string, bool) {
	if s.token == tkLabel {
		st := s.rawText()
		return st[:len(st)-1], true
	}
	return "", false
}

func (s *scanner) asStringLiteral() (string, bool) {
	if s.token == tkStringLiteral {
		st := s.rawText()
		return unescape(st[1 : len(st)-1]), true
	}
	return "", false
}

func (s *scanner) end() bool {
	return s.state == scEnd || s.state == scError
}

func escape(val string) string {
	return strings.Replace(val, "\"", "\\\"", -1)
}

func unescape(val string) string {
	return strings.Replace(val, "\\", "", -1)
}
