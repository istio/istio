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
	"regexp"
	"strings"
	"text/scanner"
	"unicode"
)

const (
	tokenVerifierKey = "token"
)

// verifier for output of a shell command.
type verifier func(ctx Context, name, expectedOutput, actualOutput string)

// verifiers supported by in the command scripts.
var verifiers = map[string]verifier{
	tokenVerifierKey: verifyTokens,
	"contains":       verifyContains,
	"notContains":    verifyNotContains,
	"lineRegex":      verifyLineRegex,
}

// verifyTokens tokenizes the output and compares against the tokens from the given file.
func verifyTokens(ctx Context, name, expectedOutput, actualOutput string) {
	// Tokenize the content and the file.
	expectedTokenLines := tokenize(expectedOutput)
	actualTokenLines := tokenize(actualOutput)

	if len(expectedTokenLines) != len(actualTokenLines) {
		ctx.Fatalf("verification failed for command %s: line count: (expected %d, found %d). Expected:\n%s\nto match:\n%s",
			name, len(expectedTokenLines), len(actualTokenLines), actualOutput, expectedOutput)
	}

	for lineIndex := 0; lineIndex < len(expectedTokenLines); lineIndex++ {
		expectedTokens := expectedTokenLines[lineIndex]
		actualTokens := actualTokenLines[lineIndex]

		if len(expectedTokens) != len(actualTokens) {
			ctx.Fatalf("verification failed for command %s [line %d]: token count (expected %d, found %d). Expected:\n%s\nto match:\n%s",
				name, lineIndex, len(expectedTokens), len(actualTokens), actualOutput, expectedOutput)
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
					name, lineIndex, tokenIndex, expectedToken, actualToken, actualOutput, expectedOutput)
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

func verifyContains(ctx Context, name, expectedOutput, actualOutput string) {
	if !strings.Contains(actualOutput, expectedOutput) {
		ctx.Fatalf("verification failed for command %s: output does not contain expected text.\nExpected:\n%s\nOutput:\n%s",
			name, expectedOutput, actualOutput)
	}
}

func verifyNotContains(ctx Context, name, expectedOutput, actualOutput string) {
	if strings.Contains(actualOutput, expectedOutput) {
		ctx.Fatalf("verification failed for command %s: output contains not expected text.\nNot Expected:\n%s\nOutput:\n%s",
			name, expectedOutput, actualOutput)
	}
}

func verifyLineRegex(ctx Context, name, expectedOutput, actualOutput string) {
	expectedOutputLines := strings.Split(strings.TrimSpace(expectedOutput), "\n")
	actualOutputLines := strings.Split(strings.TrimSpace(actualOutput), "\n")

	if len(expectedOutputLines) != len(actualOutputLines) {
		ctx.Fatalf("verification failed for command %s: line count: (expected %d, found %d). Expected:\n%s\nto match:\n%s",
			name, len(expectedOutputLines), len(actualOutputLines), actualOutput, expectedOutput)
	}

	for lineIndex := 0; lineIndex < len(expectedOutputLines); lineIndex++ {
		if match, _ := regexp.MatchString(expectedOutputLines[lineIndex], actualOutputLines[lineIndex]); !match {
			ctx.Fatalf("verification failed for command %s: output does not match expected regex.\nExpected:\n%s\nOutput:\n%s",
				name, expectedOutputLines[lineIndex], actualOutputLines[lineIndex])
			return
		}
	}
}
