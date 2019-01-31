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

package parser

import (
	"testing"
)

func TestUnescapeSingleQuote(t *testing.T) {
	text, err := unescape(`'hello'`)
	if err != nil {
		t.Error(err)
	}
	if text != "hello" {
		t.Errorf("Got '%v', wanted '%v", text, "hello")
	}
}

func TestUnescapeDoubleQuote(t *testing.T) {
	text, err := unescape(`""`)
	if err != nil {
		t.Error(err)
	}
	if text != `` {
		t.Errorf("Got '%v', wanted '%v'", text, ``)
	}
}

func TestUnescapeEscapedQuote(t *testing.T) {
	// The argument to unescape is dquote-backslash-dquote-dquote where both
	// the backslash and inner double-quote are escaped.
	text, err := unescape(`"\\\""`)
	if err != nil {
		t.Error(err)
	}
	if text != `\"` {
		t.Errorf("Got '%v', wanted '%v'", text, `\"`)
	}
}

func TestUnescapeEscapedEscape(t *testing.T) {
	text, err := unescape(`"\\"`)
	if err != nil {
		t.Error(err)
	}
	if text != `\` {
		t.Errorf("Got '%v', wanted '%v'", text, `\`)
	}
}

func TestUnescapeTripleSingleQuote(t *testing.T) {
	text, err := unescape(`'''x''x'''`)
	if err != nil {
		t.Error(err)
	}
	if text != `x''x` {
		t.Errorf("Got '%v', wanted '%v'", text, `x''x`)
	}
}

func TestUnescapeTripleDoubleQuote(t *testing.T) {
	text, err := unescape(`"""x""x"""`)
	if err != nil {
		t.Error(err)
	}
	if text != `x""x` {
		t.Errorf("Got '%v', wanted '%v'", text, `x""x`)
	}
}

func TestUnescapeMultiOctalSequence(t *testing.T) {
	// Octal 303 -> Code point 195 (Ã)
	// Octal 277 -> Code point 191 (¿)
	text, err := unescape(`"\303\277"`)
	if err != nil {
		t.Error(err)
	}
	if text != `Ã¿` {
		t.Errorf("Got '%v', wanted '%v'", text, `Ã¿`)
	}
}

func TestUnescapeOctalSequence(t *testing.T) {
	// Octal 377 -> Code point 255 (ÿ)
	text, err := unescape(`"\377"`)
	if err != nil {
		t.Error(err)
	}
	if text != `ÿ` {
		t.Errorf("Got '%v', wanted '%v'", text, `ÿ`)
	}
}

func TestUnescapeUnicodeSequence(t *testing.T) {
	text, err := unescape(`"\u263A\u263A"`)
	if err != nil {
		t.Error(err)
	}
	if text != `☺☺` {
		t.Errorf("Got '%v', wanted '%v'", text, `☺☺`)
	}
}

func TestUnescapeLegalEscapes(t *testing.T) {
	text, err := unescape(`"\a\b\f\n\r\t\v\'\"\\\? Legal escapes"`)
	if err != nil {
		t.Error(err)
	}
	if text != "\a\b\f\n\r\t\v'\"\\? Legal escapes" {
		t.Errorf("Got '%v', wanted '%v'", text, "\a\b\f\n\r\t\v'\"\\? Legal escapes")
	}
}

func TestUnescapeIllegalEscapes(t *testing.T) {
	// The first escape sequences are legal, but the '\>' is not.
	text, err := unescape(`"\a\b\f\n\r\t\v\'\"\\\? Illegal escape \>"`)
	if err == nil {
		t.Errorf("Got '%v', expected error", text)
	}
}
