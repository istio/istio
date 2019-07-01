// Package vfstemplate offers html/template helpers that use http.FileSystem.
package vfstemplate

import (
	"fmt"
	"html/template"
	"net/http"
	"path"

	"github.com/shurcooL/httpfs/path/vfspath"
	"github.com/shurcooL/httpfs/vfsutil"
)

// ParseFiles creates a new Template if t is nil and parses the template definitions from
// the named files. The returned template's name will have the (base) name and
// (parsed) contents of the first file. There must be at least one file.
// If an error occurs, parsing stops and the returned *Template is nil.
func ParseFiles(fs http.FileSystem, t *template.Template, filenames ...string) (*template.Template, error) {
	return parseFiles(fs, t, filenames...)
}

// ParseGlob parses the template definitions in the files identified by the
// pattern and associates the resulting templates with t. The pattern is
// processed by vfspath.Glob and must match at least one file. ParseGlob is
// equivalent to calling t.ParseFiles with the list of files matched by the
// pattern.
func ParseGlob(fs http.FileSystem, t *template.Template, pattern string) (*template.Template, error) {
	filenames, err := vfspath.Glob(fs, pattern)
	if err != nil {
		return nil, err
	}
	if len(filenames) == 0 {
		return nil, fmt.Errorf("vfs/html/vfstemplate: pattern matches no files: %#q", pattern)
	}
	return parseFiles(fs, t, filenames...)
}

// parseFiles is the helper for the method and function. If the argument
// template is nil, it is created from the first file.
func parseFiles(fs http.FileSystem, t *template.Template, filenames ...string) (*template.Template, error) {
	if len(filenames) == 0 {
		// Not really a problem, but be consistent.
		return nil, fmt.Errorf("vfs/html/vfstemplate: no files named in call to ParseFiles")
	}
	for _, filename := range filenames {
		b, err := vfsutil.ReadFile(fs, filename)
		if err != nil {
			return nil, err
		}
		s := string(b)
		name := path.Base(filename)
		// First template becomes return value if not already defined,
		// and we use that one for subsequent New calls to associate
		// all the templates together. Also, if this file has the same name
		// as t, this file becomes the contents of t, so
		//  t, err := New(name).Funcs(xxx).ParseFiles(name)
		// works. Otherwise we create a new template associated with t.
		var tmpl *template.Template
		if t == nil {
			t = template.New(name)
		}
		if name == t.Name() {
			tmpl = t
		} else {
			tmpl = t.New(name)
		}
		_, err = tmpl.Parse(s)
		if err != nil {
			return nil, err
		}
	}
	return t, nil
}
