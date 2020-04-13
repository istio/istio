package features


// TODO: this file should be generated from YAML to make it more easy to modify.

// WARNING: changes to existing elements in this file will cause corruption of test coverage data.
// don't change existing entries unless absolutely necessary

//go:generate stringer -type=Feature

type Feature int

const (
	USABILITY_OBSERVABILITY_STATUS Feature =iota
)