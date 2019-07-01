// Package vfspath implements utility routines for manipulating virtual file system paths.
package vfspath

import (
	"net/http"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/shurcooL/httpfs/vfsutil"
)

const separator = "/"

// Glob returns the names of all files matching pattern or nil
// if there is no matching file. The syntax of patterns is the same
// as in path.Match. The pattern may describe hierarchical names such as
// /usr/*/bin/ed.
//
// Glob ignores file system errors such as I/O errors reading directories.
// The only possible returned error is ErrBadPattern, when pattern
// is malformed.
func Glob(fs http.FileSystem, pattern string) (matches []string, err error) {
	if !hasMeta(pattern) {
		if _, err = vfsutil.Stat(fs, pattern); err != nil {
			return nil, nil
		}
		return []string{pattern}, nil
	}

	dir, file := path.Split(pattern)
	switch dir {
	case "":
		dir = "."
	case string(separator):
		// nothing
	default:
		dir = dir[0 : len(dir)-1] // chop off trailing separator
	}

	if !hasMeta(dir) {
		return glob(fs, dir, file, nil)
	}

	var m []string
	m, err = Glob(fs, dir)
	if err != nil {
		return
	}
	for _, d := range m {
		matches, err = glob(fs, d, file, matches)
		if err != nil {
			return
		}
	}
	return
}

// glob searches for files matching pattern in the directory dir
// and appends them to matches. If the directory cannot be
// opened, it returns the existing matches. New matches are
// added in lexicographical order.
func glob(fs http.FileSystem, dir, pattern string, matches []string) (m []string, e error) {
	m = matches
	fi, err := vfsutil.Stat(fs, dir)
	if err != nil {
		return
	}
	if !fi.IsDir() {
		return
	}
	fis, err := vfsutil.ReadDir(fs, dir)
	if err != nil {
		return
	}

	sort.Sort(byName(fis))

	for _, fi := range fis {
		n := fi.Name()
		matched, err := path.Match(path.Clean(pattern), n)
		if err != nil {
			return m, err
		}
		if matched {
			m = append(m, path.Join(dir, n))
		}
	}
	return
}

// hasMeta reports whether path contains any of the magic characters
// recognized by Match.
func hasMeta(path string) bool {
	// TODO(niemeyer): Should other magic characters be added here?
	return strings.ContainsAny(path, "*?[")
}

// byName implements sort.Interface.
type byName []os.FileInfo

func (f byName) Len() int           { return len(f) }
func (f byName) Less(i, j int) bool { return f[i].Name() < f[j].Name() }
func (f byName) Swap(i, j int)      { f[i], f[j] = f[j], f[i] }
