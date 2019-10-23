// Copyright 2019 Istio Authors
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

package istioio

import (
	"strings"
	"text/scanner"
	"unicode"
)

// Verifier is a function used to verify output and any errors returned.
type Verifier func(ctx Context, name, output string, err error)

var (
	defaultVerifier = func(ctx Context, name, output string, err error) {
		if err != nil {
			ctx.Fatalf("command %s failed: %v. Output: %v", name, err, output)
		}
	}
)

// TokenVerifier tokenizes the output and compares against the tokens from the given file.
func TokenVerifier(selector InputSelector) Verifier {
	return func(ctx Context, name, output string, _ error) {
		input := selector.SelectInput(ctx)

		// Read the input.
		expected, err := input.ReadAll()
		if err != nil {
			ctx.Fatalf("verification failed for command %s: %v", name, err)
		}

		// Tokenize the content and the file.
		expectedTokenLines := tokenize(expected)
		actualTokenLines := tokenize(output)

		if len(expectedTokenLines) != len(actualTokenLines) {
			ctx.Fatalf("verification failed for command %s: line count: (expected %d, found %d). Expected:\n%s\nto match:\n%s",
				name, len(expectedTokenLines), len(actualTokenLines), output, expected)
		}

		for lineIndex := 0; lineIndex < len(expectedTokenLines); lineIndex++ {
			expectedTokens := expectedTokenLines[lineIndex]
			actualTokens := actualTokenLines[lineIndex]

			if len(expectedTokens) != len(actualTokens) {
				ctx.Fatalf("verification failed for command %s [line %d]: token count (expected %d, found %d). Expected:\n%s\nto match:\n%s",
					name, lineIndex, len(expectedTokens), len(actualTokens), output, expected)
			}

			for tokenIndex := 0; tokenIndex < len(expectedTokens); tokenIndex++ {
				expectedToken := expectedTokens[tokenIndex]
				if expectedToken == "?" {
					// The value was a wildcard, matches anything.
					continue
				}

				actualToken := actualTokens[tokenIndex]
				if expectedToken != actualToken {
					ctx.Fatalf("verification failed for command %s [line %d]: token %d (expected %s, found %s). Expected:\n%s\nto match:\n%s",
						name, lineIndex, tokenIndex, expectedToken, actualToken, output, expected)
				}
			}
		}
	}
}

func tokenize(content string) [][]string {
	inputLines := strings.Split(strings.TrimSpace(content), "\n")
	tokenLines := make([][]string, 0)

	for _, inputLine := range inputLines {
		tokens := make([]string, 0)

		var s scanner.Scanner

		// Configure the token characters.
		s.IsIdentRune = func(ch rune, i int) bool {
			return ch == '_' || ch == '-' || ch == '.' || unicode.IsLetter(ch) || unicode.IsDigit(ch)
		}

		s.Init(strings.NewReader(inputLine))
		for tok := s.Scan(); tok != scanner.EOF; tok = s.Scan() {
			tokens = append(tokens, s.TokenText())
		}
		tokenLines = append(tokenLines, tokens)
	}
	return tokenLines
}

// ContainsVerifier checks if the output contains an expected value
func ContainsVerifier(selector InputSelector) Verifier {
	return internalContainsOrNot(selector, true)
}

// NotContainsVerifier checks if the output does not contain a value
func NotContainsVerifier(selector InputSelector) Verifier {
	return internalContainsOrNot(selector, false)
}

func internalContainsOrNot(selector InputSelector, mustContain bool) Verifier {
	return func(ctx Context, name, output string, _ error) {
		input := selector.SelectInput(ctx)

		// Read the input.
		expected, err := input.ReadAll()
		if err != nil {
			ctx.Fatalf("verification failed for command %s: %v", name, err)
		}

		expected = strings.TrimSpace(expected)

		contains := strings.Contains(output, expected)
		if !contains && mustContain {
			ctx.Fatalf("verification failed for command %s: output does not contain expected text.\nExpected:\n%s\nOutput:\n%s",
				name, expected, output)
			return
		}
		if contains && !mustContain {
			ctx.Fatalf("verification failed for command %s: output contains not expected text.\nNot Expected:\n%s\nOutput:\n%s",
				name, expected, output)
			return
		}
	}
}
