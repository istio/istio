package testdata

type Test struct {
	a int
	b string
}

func MakeTest(a int, b string) Test {
	return Test{a, b}
}
