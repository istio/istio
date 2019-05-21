package util

import (
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v2"
)

const (
	pathSeparator = "."
)

// Path is a path in slice form.
type Path []string

// PathFromString converts a string path of form a.b.c to a string slice representation.
func PathFromString(path string) []string {
	path = filepath.Clean(path)
	path = strings.TrimPrefix(path, pathSeparator)
	path = strings.TrimSuffix(path, pathSeparator)
	pv := strings.Split(path, pathSeparator)
	var r []string
	for _, str := range pv {
		if str != "" {
			r = append(r, str)
		}
	}
	return r
}

// String converts a string slice path representation of form ["a", "b", "c"] to a string representation like "a.b.c".
func (p Path) String() string {
	return strings.Join(p, pathSeparator)
}

// Tree is a tree.
type Tree map[string]interface{}

// String implements the Stringer interface method.
func (t Tree) String() string {
	y, err := yaml.Marshal(t)
	if err != nil {
		return err.Error()
	}
	return string(y)
}
