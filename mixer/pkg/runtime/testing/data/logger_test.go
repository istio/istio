//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package data

import (
	"testing"
)

func TestLogger_String(t *testing.T) {
	l := Logger{}
	checkLoggerContains(t, &l, "")
}

func TestLogger_String_Nil(t *testing.T) {
	var l *Logger
	checkLoggerContains(t, l, "")
}

func TestLogger_Write(t *testing.T) {
	l := Logger{}
	l.Write("foo", "bar")
	checkLoggerContains(t, &l, "[foo] bar\n")
}

func TestLogger_Write_Nil(t *testing.T) {
	var l *Logger
	l.Write("foo", "bar")
	checkLoggerContains(t, l, "")
}

func TestLogger_Write_Multiline(t *testing.T) {
	l := Logger{}
	l.Write("foo", "bar")
	l.Write("boo", "far")
	checkLoggerContains(t, &l, "[foo] bar\n[boo] far\n")
}

func TestLogger_WriteFormat(t *testing.T) {
	l := Logger{}
	l.WriteFormat("foo", "bar %s", "baz")
	checkLoggerContains(t, &l, "[foo] bar baz\n")
}

func TestLogger_WriteFormat_Nil(t *testing.T) {
	var l *Logger
	l.WriteFormat("foo", "bar %s", "baz")
	checkLoggerContains(t, l, "")
}

func TestLogger_WriteFormat_Multiline(t *testing.T) {
	l := Logger{}
	l.WriteFormat("foo", "bar %s", "baz")
	l.WriteFormat("boo", "far %s", "gaz")
	checkLoggerContains(t, &l, "[foo] bar baz\n[boo] far gaz\n")
}

func TestLogger_Clear(t *testing.T) {
	l := Logger{}
	l.WriteFormat("foo", "bar %s", "baz")
	l.WriteFormat("boo", "far %s", "gaz")
	l.Clear()
	checkLoggerContains(t, &l, "")
}

func TestLogger_Clear_Nil(t *testing.T) {
	var l *Logger
	l.WriteFormat("foo", "bar %s", "baz")
	l.WriteFormat("boo", "far %s", "gaz")
	l.Clear()
	checkLoggerContains(t, l, "")
}

func checkLoggerContains(t *testing.T, l *Logger, expected string) {
	if l.String() != expected {
		t.Fatalf("Unexpected logger state: got:\n'%s'\nwanted:\n'%s'\n", l.String(), expected)
	}
}
