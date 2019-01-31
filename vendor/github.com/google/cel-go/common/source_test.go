// Copyright 2018 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// package common defines types common to parsing and other diagnostics.
package common

import (
	"testing"
)

const (
	unexpectedValue   = "%s got snippet '%v', want '%v'"
	unexpectedSnippet = "%s got snippet '%s', want '%v'"
	snippetNotFound   = "%s snippet not found, wanted '%v'"
	snippetFound      = "%s snippet found at line %d, wanted none"
)

// TestStringSource_Description the error description method.
func TestStringSource_Description(t *testing.T) {
	contents := "example content\nsecond line"
	source := NewStringSource(contents, "description-test")
	// Verify the content
	if source.Content() != contents {
		t.Errorf(unexpectedValue, t.Name(), contents, source.Content())
	}
	// Verify the description
	if source.Description() != "description-test" {
		t.Errorf(unexpectedValue, t.Name(), source.Description(), "description-test")
	}

	// Assert that the snippets on lines 1 & 2 are what was expected.
	if str2, found := source.Snippet(2); !found {
		t.Errorf(snippetNotFound, t.Name(), 2)

	} else if str2 != "second line" {
		t.Errorf(unexpectedSnippet, t.Name(), str2, "second line")
	}
	if str1, found := source.Snippet(1); !found {
		t.Errorf(snippetNotFound, t.Name(), 1)

	} else if str1 != "example content" {
		t.Errorf(unexpectedSnippet, t.Name(), str1, "example content")
	}
}

// TestStringSource_LocationOffset make sure that the offsets accurately reflect
// the location of a character in source.
func TestStringSource_LocationOffset(t *testing.T) {
	contents := "c.d &&\n\t b.c.arg(10) &&\n\t test(10)"
	source := NewStringSource(contents, "offset-test")
	expectedLineOffsets := []int32{7, 24, 35}
	if len(expectedLineOffsets) != len(source.LineOffsets()) {
		t.Errorf("Got list of size '%d', wanted size '%d'",
			len(source.LineOffsets()), len(expectedLineOffsets))
	} else {
		for i, val := range expectedLineOffsets {
			if val != source.LineOffsets()[i] {
				t.Errorf("Got %d, wanted line %d offset of %d, ",
					val, source.LineOffsets()[i], i)
			}
		}
	}
	// Ensure that selecting a set of characters across multiple lines works as
	// expected.
	charStart, _ := source.LocationOffset(NewLocation(1, 2))
	charEnd, _ := source.LocationOffset(NewLocation(3, 2))
	if "d &&\n\t b.c.arg(10) &&\n\t " != string(contents[charStart:charEnd]) {
		t.Errorf(unexpectedValue, t.Name(),
			string(contents[charStart:charEnd]),
			"d &&\n\t b.c.arg(10) &&\n\t ")
	}
	if _, found := source.LocationOffset(NewLocation(4, 0)); found {
		t.Error("Character offset was out of range of source, but still found.")
	}
}

// TestStringSource_SnippetMultiline snippets of text from a multiline source.
func TestStringSource_SnippetMultiline(t *testing.T) {
	source := NewStringSource("hello\nworld\nmy\nbub\n", "four-line-test")
	if str, found := source.Snippet(1); !found {
		t.Errorf(snippetNotFound, t.Name(), 1)
	} else if str != "hello" {
		t.Errorf(unexpectedSnippet, t.Name(), str, "hello")
	}
	if str2, found := source.Snippet(2); !found {
		t.Errorf(snippetNotFound, t.Name(), 2)
	} else if str2 != "world" {
		t.Errorf(unexpectedSnippet, t.Name(), str2, "world")
	}
	if str3, found := source.Snippet(3); !found {
		t.Errorf(snippetNotFound, t.Name(), 3)
	} else if str3 != "my" {
		t.Errorf(unexpectedSnippet, t.Name(), str3, "my")
	}
	if str4, found := source.Snippet(4); !found {
		t.Errorf(snippetNotFound, t.Name(), 4)
	} else if str4 != "bub" {
		t.Errorf(unexpectedSnippet, t.Name(), str4, "bub")
	}
	if str5, found := source.Snippet(5); !found {
		t.Errorf(snippetNotFound, t.Name(), 5)
	} else if str5 != "" {
		t.Errorf(unexpectedSnippet, t.Name(), str5, "")
	}
}

// TestStringSource_SnippetSingleline snippets from a single line source.
func TestStringSource_SnippetSingleline(t *testing.T) {
	source := NewStringSource("hello, world", "one-line-test")
	if str, found := source.Snippet(1); !found {
		t.Errorf(snippetNotFound, t.Name(), 1)

	} else if str != "hello, world" {
		t.Errorf(unexpectedSnippet, t.Name(), str, "hello, world")
	}
	if str2, found := source.Snippet(2); found {
		t.Error(snippetFound, t.Name(), 2)
	} else if str2 != "" {
		t.Error(unexpectedSnippet, t.Name(), str2, "")
	}
}
