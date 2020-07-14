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

package descriptor

import (
	"testing"

	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
)

var (
	comment1 = `this is
test
comment 1`
	comment2 = `this is
test
comment 2`
	testFileDescriptor = FileDescriptor{
		lineNumbers: map[string]string{
			"ln-1": "12",
			"ln-2": "22",
		},
		comments: map[string]*descriptor.SourceCodeInfo_Location{
			"cmt-1": {LeadingComments: &comment1},
			"cmt-2": {LeadingComments: &comment2},
		},
	}
)

func TestFileDescriptor_GetComment(t *testing.T) {
	tests := []struct {
		name, path, want string
	}{
		{
			"get correct string",
			"cmt-1",
			`// this is
// test
// comment 1`,
		},
		{
			"wrong path",
			"cmt-3",
			"",
		},
	}

	for _, tc := range tests {
		if got := testFileDescriptor.GetComment(tc.path); got != tc.want {
			t.Errorf("GetComment(%s) = %s, want %s", tc.path, got, tc.want)
		}
	}
}

func TestFileDescriptor_GetLineNumber(t *testing.T) {
	tests := []struct {
		name, ln, want string
	}{
		{
			"get correct linenumber",
			"ln-1",
			"12",
		},
		{
			"wrong path",
			"ln-3",
			"",
		},
	}
	for _, tc := range tests {
		if got := testFileDescriptor.GetLineNumber(tc.ln); got != tc.want {
			t.Errorf("GetComment(%s) = %s, want %s", tc.ln, got, tc.want)
		}
	}
}

func TestCamelCase(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"one", "One"},
		{"one_two", "OneTwo"},
		{"_my_field_name_2", "XMyFieldName_2"},
		{"Something_Capped", "Something_Capped"},
		{"my_Name", "My_Name"},
		{"OneTwo", "OneTwo"},
		{"_", "X"},
		{"_a_", "XA_"},
	}
	for _, tc := range tests {
		if got := CamelCase(tc.in); got != tc.want {
			t.Errorf("CamelCase(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
