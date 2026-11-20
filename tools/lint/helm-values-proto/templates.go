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

package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"

	"istio.io/istio/operator/pkg/apis" // nolint: depguard
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

// valuesRef matches a `.Values.a.b.c` chain. Dynamic lookups, such as
// `index .Values "a" "b"`, are not matched and so not checked.
var valuesRef = regexp.MustCompile(`\$?\.Values((?:\.[A-Za-z_][A-Za-z0-9_]*)+)`)

// parenValuesRef matches the nil-safe `($.Values.a).b`, equivalent to `.Values.a.b`.
var parenValuesRef = regexp.MustCompile(`\(\s*\$?\.Values((?:\.[A-Za-z_][A-Za-z0-9_]*)*)\s*\)((?:\.[A-Za-z_][A-Za-z0-9_]*)+)`)

// templateComment matches `{{/* ... */}}`, which may mention values nothing reads.
var templateComment = regexp.MustCompile(`(?s)\{\{-?\s*/\*.*?\*/\s*-?\}\}`)

// freeFormMessages hold arbitrary user data, so any path below them is valid.
var freeFormMessages = sets.New(
	"google.protobuf.Struct",
	"google.protobuf.Value",
	"google.protobuf.ListValue",
	"google.protobuf.Any",
)

// ignoredTemplateRefs are Helm plumbing rather than values.
var ignoredTemplateRefs = sets.New(
	"_internal_defaults_do_not_set", // the chart defaults, see values.yaml
	"_original",                     // the user input, stashed by zzz_profile.yaml
	"defaults",                      // where zzz_profile.yaml stages the defaults
)

// allowedMissingRefs are paths the charts read that are deliberately not in the schema.
var allowedMissingRefs = sets.New(
	// zzz_profile.yaml reads this nil-safely as a legacy alias of `profile`.
	"global.profile",
)

type templateRef struct {
	chart string
	file  string
	line  int
	path  string
}

// lintTemplates checks that every `.Values` reference in the charts resolves to a field in
// the Values schema. The values.yaml check cannot see values a chart reads but never defaults.
func lintTemplates() []string {
	values := (&apis.Values{}).ProtoReflect().Descriptor()
	problems := []string{}
	for _, chartPath := range slices.Sort(maps.Keys(chartToValuesField)) {
		refs, err := collectRefs(chartPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: scanning templates: %v\n", err)
			os.Exit(1)
		}
		for _, ref := range refs {
			if allowedMissingRefs.Contains(ref.path) {
				continue
			}
			if err := resolveRef(values, chartToValuesField[ref.chart], ref.path); err != nil {
				problems = append(problems, fmt.Sprintf("%s:%d: .Values.%s: %v", ref.file, ref.line, ref.path, err))
			}
		}
	}
	return problems
}

// collectRefs returns the `.Values` references in the templates of the chart at chartPath
// (relative to manifests/charts), keeping the first occurrence of each path.
func collectRefs(chartPath string) ([]templateRef, error) {
	dir := filepath.Join("manifests/charts", chartPath, "templates")
	var refs []templateRef
	seen := sets.New[string]()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(blankComments(string(data)), "\n") {
			for _, p := range refsInLine(line) {
				if ignoredTemplateRefs.Contains(strings.Split(p, ".")[0]) {
					continue
				}
				if key := chartPath + "|" + p; !seen.InsertContains(key) {
					refs = append(refs, templateRef{chart: chartPath, file: path, line: i + 1, path: p})
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return refs, nil
}

// blankComments blanks out template comments, keeping the line numbering intact.
func blankComments(data string) string {
	return templateComment.ReplaceAllStringFunc(data, func(comment string) string {
		return strings.Map(func(r rune) rune {
			if r == '\n' {
				return r
			}
			return ' '
		}, comment)
	})
}

// refsInLine returns the value paths, without the leading `.Values.`, read by one line.
func refsInLine(line string) []string {
	var paths []string
	for _, m := range valuesRef.FindAllStringSubmatch(line, -1) {
		paths = append(paths, strings.TrimPrefix(m[1], "."))
	}
	for _, m := range parenValuesRef.FindAllStringSubmatch(line, -1) {
		paths = append(paths, strings.TrimPrefix(m[1]+m[2], "."))
	}
	return paths
}

// resolveRef resolves path for a chart whose values live under fieldPath in spec.values.
// The chart's own values are nested there (istio-discovery's `.Values.hub` is `pilot.hub`),
// but shared ones such as `.Values.global` are read as written, so either is accepted.
func resolveRef(values protoreflect.MessageDescriptor, fieldPath, path string) error {
	segments := strings.Split(path, ".")
	err := resolvePath(values, segments)
	if err == nil || fieldPath == "" {
		return err
	}
	nested := resolvePath(values, append([]string{fieldPath}, segments...))
	if nested == nil {
		return nil
	}
	// Report the spelling the value would have: shared ones as written, the rest nested.
	if fieldByName(values, segments[0]) != nil {
		return err
	}
	return fmt.Errorf("%v (values.%s is not in the schema either)", nested, path)
}

// resolvePath walks segments down the message, reporting the first one that is not a field.
func resolvePath(md protoreflect.MessageDescriptor, segments []string) error {
	for i, segment := range segments {
		field := fieldByName(md, segment)
		if field == nil {
			return fmt.Errorf("%q is not a field of %s", strings.Join(segments[:i+1], "."), md.FullName())
		}
		if i == len(segments)-1 {
			return nil
		}
		switch {
		case field.IsMap(), field.IsList():
			// Entries are keyed or indexed by the chart, not named in the schema.
			return nil
		case field.Kind() == protoreflect.MessageKind:
			if freeFormMessages.Contains(string(field.Message().FullName())) {
				return nil
			}
			md = field.Message()
		default:
			return fmt.Errorf("%q is a %s, so %q cannot be read from it",
				strings.Join(segments[:i+1], "."), field.Kind(), segments[i+1])
		}
	}
	return nil
}

// fieldByName looks a field up by proto name and by JSON name, which differ for `istio_cni`.
func fieldByName(md protoreflect.MessageDescriptor, name string) protoreflect.FieldDescriptor {
	if f := md.Fields().ByName(protoreflect.Name(name)); f != nil {
		return f
	}
	return md.Fields().ByJSONName(name)
}
