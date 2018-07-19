package testhelpers

func assertNoError(err error) {
	if err != nil {
		panic(err)
	}
}
