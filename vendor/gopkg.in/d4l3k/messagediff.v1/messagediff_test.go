package messagediff

import (
	"testing"
	"time"

	"github.com/d4l3k/messagediff/testdata"
)

type testStruct struct {
	A, b int
	C    []int
	D    [3]int
}

type RecursiveStruct struct {
	Key   int
	Child *RecursiveStruct
}

func newRecursiveStruct(key int) *RecursiveStruct {
	a := &RecursiveStruct{
		Key: key,
	}
	b := &RecursiveStruct{
		Key:   key,
		Child: a,
	}
	a.Child = b
	return a
}

type testCase struct {
	a, b  interface{}
	diff  string
	equal bool
}

func checkTestCases(t *testing.T, testData []testCase) {
	for i, td := range testData {
		diff, equal := PrettyDiff(td.a, td.b)
		if diff != td.diff {
			t.Errorf("%d. PrettyDiff(%#v, %#v) diff = %#v; not %#v", i, td.a, td.b, diff, td.diff)
		}
		if equal != td.equal {
			t.Errorf("%d. PrettyDiff(%#v, %#v) equal = %#v; not %#v", i, td.a, td.b, equal, td.equal)
		}
	}
}

func TestPrettyDiff(t *testing.T) {
	testData := []testCase{
		{
			true,
			false,
			"modified:  = false\n",
			false,
		},
		{
			true,
			0,
			"modified:  = 0\n",
			false,
		},
		{
			[]int{0, 1, 2},
			[]int{0, 1, 2, 3},
			"added: [3] = 3\n",
			false,
		},
		{
			[]int{0, 1, 2, 3},
			[]int{0, 1, 2},
			"removed: [3] = 3\n",
			false,
		},
		{
			[]int{0},
			[]int{1},
			"modified: [0] = 1\n",
			false,
		},
		{
			&[]int{0},
			&[]int{1},
			"modified: [0] = 1\n",
			false,
		},
		{
			map[string]int{"a": 1, "b": 2},
			map[string]int{"b": 4, "c": 3},
			"added: [\"c\"] = 3\nmodified: [\"b\"] = 4\nremoved: [\"a\"] = 1\n",
			false,
		},
		{
			testStruct{1, 2, []int{1}, [3]int{4, 5, 6}},
			testStruct{1, 3, []int{1, 2}, [3]int{4, 5, 6}},
			"added: .C[1] = 2\nmodified: .b = 3\n",
			false,
		},
		{
			nil,
			nil,
			"",
			true,
		},
		{
			&struct{}{},
			nil,
			"modified:  = <nil>\n",
			false,
		},
		{
			nil,
			&struct{}{},
			"modified:  = &struct {}{}\n",
			false,
		},
		{
			time.Time{},
			time.Time{},
			"",
			true,
		},
		{
			testdata.MakeTest(10, "duck"),
			testdata.MakeTest(20, "foo"),
			"modified: .a = 20\nmodified: .b = \"foo\"\n",
			false,
		},
	}
	checkTestCases(t, testData)
}

func TestPrettyDiffRecursive(t *testing.T) {
	testData := []testCase{
		{
			newRecursiveStruct(1),
			newRecursiveStruct(1),
			"",
			true,
		},
		{
			newRecursiveStruct(1),
			newRecursiveStruct(2),
			"modified: .Child.Key = 2\nmodified: .Key = 2\n",
			false,
		},
	}
	checkTestCases(t, testData)
}

func TestPathString(t *testing.T) {
	testData := []struct {
		in   Path
		want string
	}{{
		Path{StructField("test"), SliceIndex(1), MapKey{"blue"}, MapKey{12.3}},
		".test[1][\"blue\"][12.3]",
	}}
	for i, td := range testData {
		if out := td.in.String(); out != td.want {
			t.Errorf("%d. %#v.String() = %#v; not %#v", i, td.in, out, td.want)
		}
	}
}

type ignoreStruct struct {
	A int `testdiff:"ignore"`
	a int
	B [3]int `testdiff:"ignore"`
	b [3]int
}

func TestIgnoreTag(t *testing.T) {
	s1 := ignoreStruct{1, 1, [3]int{1, 2, 3}, [3]int{4, 5, 6}}
	s2 := ignoreStruct{2, 1, [3]int{1, 8, 3}, [3]int{4, 5, 6}}

	diff, equal := PrettyDiff(s1, s2)
	if !equal {
		t.Errorf("Expected structs to be equal. Diff:\n%s", diff)
	}

	s2 = ignoreStruct{2, 2, [3]int{1, 8, 3}, [3]int{4, 9, 6}}
	diff, equal = PrettyDiff(s1, s2)
	if equal {
		t.Errorf("Expected structs NOT to be equal.")
	}
	expect := "modified: .a = 2\nmodified: .b[1] = 9\n"
	if diff != expect {
		t.Errorf("Expected diff to be:\n%v\nbut got:\n%v", expect, diff)
	}
}
