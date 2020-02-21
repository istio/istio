/*
yaml_text.go provides tools for adding/replacing YAML paths which preserves key ordering and all file decorations
like comments and whitespace.
*/

package util

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/ghodss/yaml"

	"istio.io/pkg/log"
)

const (
	yamlSeparator = "---"
)

var (
	// tabSize is the global tab size for the YAML text being processed.
	tabSize      = 0
	commentRegex = regexp.MustCompile("#")
)

// Rule is a YAML text processing rule. Rule lists are applied sequentially, first to last.
// The default rule action is to add after the matching path.
type Rule struct {
	// Path is the path that the rule applies to.
	Path Path
	// Op is the operation to perform on the matching path.
	Op Op
	// Position is the position to apply the operation, relative to the matching line.
	Position Position
	// Key is the key to add, for add operations.
	Key string
	// Val is the value to add, for add operations.
	Val string
}

// Position indicates a relative position for text insertion.
type Position int

const (
	AFTER Position = iota
	AT
	BEFORE
)

// Op is an add/replace/delete operation type.
type Op int

const (
	ADD Op = iota
	REPLACE
	// TODO: implement DELETE
)

// pathChange tracks whether a path has become shorted, longer or is unchanged.
type pathChange int

const (
	NONE pathChange = iota
	PUSH
	POP
)

// state tracks internal text processing state.
type state struct {
	path, prevPath Path
	pathChange     pathChange
	indent         int
}

func newState() *state {
	return &state{
		// -ve indent means we haven't hit the object root yet.
		indent: -1 * tabSize,
	}
}

// ProcessYAMLText applies the given rules in the given order to the given manifest string and returns the resulting
// manifest.
func ProcessYAMLText(manifest string, rules []*Rule) (string, error) {
	if isEmptyManifest(manifest) {
		return manifest, nil
	}
	var err error
	tabSize, err = detectTabSize(manifest)
	if err != nil {
		return "", err
	}
	ymls, err := splitManifest(manifest)
	if err != nil {
		return "", err
	}

	var newYamls []string
	for _, yml := range ymls {
		var newYaml []string
		// Tree is required to know whether a path already exists somewhere in the resource.
		tree := make(map[string]interface{})
		if err := yaml.Unmarshal([]byte(yml), &tree); err != nil {
			return "", err
		}
		for _, r := range rules {
			newYaml = []string{}
			s := newState()
			lprev := ""
			for _, l := range strings.Split(yml, "\n") {
				// Preprocess the line to handle comments and whitespace.
				lc := removeComments(l)
				if spaceOnly(lc) {
					newYaml = append(newYaml, l)
					continue
				}

				// Update indent state.
				s.indent = numLeadingSpaces(lc)
				indentJump, err := getIndentJump(lprev, lc)
				if err != nil {
					return "", fmt.Errorf("indentation problem: %s\n%s\n%s", err, lprev, l)
				}

				// ProcessYAMLText key-value.
				k, _ := getKV(l)

				// Update the path.
				s.pathChange, err = updatePath(s, indentJump, k)
				if err != nil {
					return "", err
				}

				// Apply the rule if it matches.
				newLines, op, position := applyRule(tree, r, s, l)

				var writeLines []string
				switch op {
				case ADD:
					switch position {
					case BEFORE:
						writeLines = append(newLines, l)
					case AFTER:
						writeLines = append([]string{l}, newLines...)
					default:
						return "", fmt.Errorf("unsupported position %v for operation %v", position, op)
					}
				case REPLACE:
					switch position {
					case AT:
						writeLines = newLines
					default:
						return "", fmt.Errorf("unsupported position %v for operation %v", position, op)
					}

				default:
					return "", fmt.Errorf("illegal operation %v", op)
				}
				newYaml = append(newYaml, writeLines...)
				lprev = l
			}
			yml = strings.Join(newYaml, "\n")
		}
		newYamls = append(newYamls, strings.Join(newYaml, "\n"))
	}

	return strings.Join(newYamls, yamlSeparator+"\n"), nil
}

// isEmptyManifest reports whether the given manifest contains any YAML data. Comments and whitespace are excluded
// before the check.
func isEmptyManifest(manifest string) bool {
	for _, l := range strings.Split(manifest, "\n") {
		// Preprocess the line to handle comments and whitespace.
		if strings.TrimSpace(removeComments(l)) != "" {
			return false
		}
	}
	return true
}

// detectTabSize attempts to detect the tab size (number of spaces per indent block) in the given YAML manifest.
// No checking for spacing errors or consistency is performed; it is assumed that the given manifest has correct,
// consistent spacing.
func detectTabSize(manifest string) (int, error) {
	yamls, err := splitManifest(manifest)
	if err != nil {
		return 0, err
	}
	for _, yaml := range yamls {
		indent := 0
		for _, l := range strings.Split(yaml, "\n") {
			lc := removeComments(l)
			if spaceOnly(lc) {
				continue
			}
			newIndent := numLeadingSpaces(lc)
			if newIndent > indent {
				return newIndent, nil
			}
		}
	}
	return 0, fmt.Errorf("could not detect tab size in manifest")
}

// splitManifest splits the given manifest into a slice of individual YAML blocks.
func splitManifest(manifest string) ([]string, error) {
	var b bytes.Buffer

	var yamls []string
	scanner := bufio.NewScanner(strings.NewReader(manifest))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == yamlSeparator {
			// yaml separator
			yamls = append(yamls, b.String())
			b.Reset()
		} else {
			if _, err := b.WriteString(line); err != nil {
				return nil, err
			}
			if _, err := b.WriteString("\n"); err != nil {
				return nil, err
			}
		}
	}
	yamls = append(yamls, b.String())
	return yamls, nil
}

// applyRule applies the given rule to the given line and returns a slice of strings that should be written at the
// given position in the manifest with the given operation.
func applyRule(tree map[string]interface{}, r *Rule, s *state, line string) ([]string, Op, Position) {
	if !s.path.Equals(r.Path) {
		// If rule doesn't match, replace the current line with itself.
		return []string{line}, REPLACE, AT
	}
	if r.Op == ADD && hasPath(tree, AppendPath(r.Path, r.Key)) {
		// If path is already in the tree, replace the current line with itself.
		return []string{line}, REPLACE, AT
	}
	vv := strings.Split(r.Val, "\n")
	var ns []string
	pad := padStr(s.indent, 1)
	padNext := padStr(s.indent, 2)
	key := r.Key
	if r.Op == REPLACE {
		key = s.path.Last()
		pad = padStr(s.indent, 0)
	}
	switch {
	case r.Val == "":
		ns = []string{fmt.Sprintf("%s%s:", pad, key)}
	case len(vv) == 1:
		ns = []string{fmt.Sprintf("%s%s: %s", pad, key, r.Val)}
	case len(vv) > 1:
		ns = []string{fmt.Sprintf("%s%s:", pad, s.path.Last())}
		for _, v := range vv {
			ns = append(ns, fmt.Sprintf("%s%s", padNext, v))
		}
	}
	return ns, r.Op, r.Position
}

// updatePath updates the state path with the given key and indentation jump.
func updatePath(s *state, indentJump int, key string) (pathChange, error) {
	ret := NONE
	s.prevPath.Copy(s.path)
	switch {
	case indentJump == 0:
		s.path.UpdateLastElem(key)
		ret = NONE
	case indentJump >= 1:
		s.path.Push(key)
		ret = PUSH
	default:
		for i := 0; i < -1*indentJump; i++ {
			s.path.Pop()
		}
		s.path.UpdateLastElem(key)
		ret = POP
	}

	return ret, nil
}

// hasPath reports whether the given tree has the given path.
func hasPath(tree map[string]interface{}, path Path) bool {
	if len(path) == 0 {
		return true
	}
	if IsValueNil(tree) {
		return false
	}
	ni, ok := tree[path[0]]
	if !ok {
		return false
	}
	if len(path) == 1 {
		return true
	}

	n, ok := ni.(map[string]interface{})
	if !ok {
		return false
	}

	return hasPath(n, path[1:])
}

// spaceOnly reports whether s contains only whitespace.
func spaceOnly(s string) bool {
	return len(strings.TrimSpace(s)) == 0
}

// removeComments returns s with anything after and including the first # character removed.
func removeComments(s string) string {
	iv := commentRegex.FindIndex([]byte(s))
	if iv == nil {
		return s
	}
	return s[:iv[0]]
}

// getIndentJump returns the number of indentation jumps between old and new indents. It returns an error if the
// positive indentation change is not one tab size.
func getIndentJump(oldLine, newLine string) (int, error) {
	oldIndent := numLeadingSpaces(oldLine)
	newIndent := numLeadingSpaces(newLine)
	if (newIndent-oldIndent)%tabSize != 0 {
		log.Warnf("bad indent change of %d:\n%s\n%s", newIndent-oldIndent, oldLine, newLine)
	}
	indentJump := (newIndent - oldIndent) / tabSize
	return indentJump, nil
}

// getKV returns the key / value pair in s if it contains one. If the line is
// a list element, it returns null strings for both key and value.
func getKV(inStr string) (string, string) {
	s := strings.TrimSpace(inStr)
	kv := strings.Split(s, ":")
	if len(kv) == 1 {
		return "", s
	}
	// If line contains more than one ':", assume it's an escaped value, not invalid YAML.
	return strings.TrimSpace(kv[0]), strings.TrimSpace(strings.Join(kv[1:], ":"))
}

// numLeadingSpaces returns the number of leading spaces in the line of text s.
func numLeadingSpaces(s string) int {
	ret := 0
	for _, c := range s {
		if c == ' ' {
			ret++
		} else {
			break
		}
	}
	return ret
}

// padStr returns a string with the given indentation, plus the given number of tabs (using global tabSize).
func padStr(indent, numTabs int) string {
	return strings.Repeat(" ", indent+numTabs*tabSize)
}
