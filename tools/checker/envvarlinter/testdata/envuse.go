package testdata

import "os"

func Envuse() {
	_ = os.Getenv("DONTDOIT")
	_, _ = os.LookupEnv("ANDDONTDOTHISEITHER")
}
