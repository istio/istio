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

package validation

import (
	"errors"
	"fmt"
)

type ParserState int32

const (
	LiteralParserState                   ParserState = iota // processing literal data
	VariableNameParserState                                 // consuming a %VAR% name
	ExpectArrayParserState                                  // expect starting [ in %VAR([...])%
	ExpectStringParserState                                 // expect starting " in array of strings
	StringParserState                                       // consuming an array element string
	ExpectArrayDelimiterOrEndParserState                    // expect array delimiter (,) or end of array (])
	ExpectArgsEndParserState                                // expect closing ) in %VAR(...)%
	ExpectVariableEndParserState                            // expect closing % in %VAR(...)%
)

// validateHeaderValue is golang port version of
// https://github.com/envoyproxy/envoy/blob/master/source/common/router/header_parser.cc#L73
func validateHeaderValue(headerValue string) error {
	if headerValue == "" {
		return nil
	}

	var (
		pos   = 0
		state = LiteralParserState
	)

	for pos < len(headerValue) {
		ch := headerValue[pos]
		hasNextCh := (pos + 1) < len(headerValue)

		switch state {
		case LiteralParserState:
			// Searching for start of %VARIABLE% expression.
			if ch != '%' {
				break
			}

			if !hasNextCh {
				return errors.New("invalid header configuration. Un-escaped %")
			}

			if headerValue[pos+1] == '%' {
				// Escaped %, skip next character.
				pos++
				break
			}

			// Un-escaped %: start of variable name. Create a formatter for preceding characters, if any.
			state = VariableNameParserState

		case VariableNameParserState:
			// Consume "VAR" from "%VAR%" or "%VAR(...)%"
			if ch == '%' {
				state = LiteralParserState
				break
			}

			if ch == '(' {
				// Variable with arguments, search for start of arg array.
				state = ExpectArrayParserState
			}

		case ExpectArrayParserState:
			// Skip over whitespace searching for the start of JSON array args.
			if ch == '[' {
				// Search for first argument string
				state = ExpectStringParserState
			} else if !isSpace(ch) {
				// Consume it as a string argument.
				state = StringParserState
			}

		case ExpectArrayDelimiterOrEndParserState:
			// Skip over whitespace searching for a comma or close bracket.
			if ch == ',' {
				state = ExpectStringParserState
			} else if ch == ']' {
				state = ExpectArgsEndParserState
			} else if !isSpace(ch) {
				return errors.New("invalid header configuration. Expecting ',', ']', or whitespace")
			}

		case ExpectStringParserState:
			// Skip over whitespace looking for the starting quote of a JSON string.
			if ch == '"' {
				state = StringParserState
			} else if !isSpace(ch) {
				return errors.New("invalid header configuration. Expecting '\"'")
			}

		case StringParserState:
			// Consume a JSON string (ignoring backslash-escaped chars).
			if ch == '\\' {
				if !hasNextCh {
					return errors.New("invalid header configuration. Un-terminated backslash in JSON string")
				}

				// Skip escaped char.
				pos++
			} else if ch == ')' {
				state = ExpectVariableEndParserState
			} else if ch == '"' {
				state = ExpectArrayDelimiterOrEndParserState
			}

		case ExpectArgsEndParserState:
			// Search for the closing paren of a %VAR(...)% expression.
			if ch == ')' {
				state = ExpectVariableEndParserState
			} else if !isSpace(ch) {
				return errors.New("invalid header configuration. Expecting ')' or whitespace after '{}', but found '{}'")
			}

		case ExpectVariableEndParserState:
			// Search for closing % of a %VAR(...)% expression
			if ch == '%' {
				state = LiteralParserState
				break
			}

			if !isSpace(ch) {
				return errors.New("invalid header configuration. Expecting '%' or whitespace after")
			}
		}

		pos++
	}

	if state != LiteralParserState {
		// Parsing terminated mid-variable.
		return fmt.Errorf("invalid header configuration. Un-terminated variable expression '%s'", headerValue)
	}

	return nil
}

func isSpace(chr byte) bool {
	return chr == ' '
}
