package ignore

import (
	"bytes"
)

// Parse reads in a gitignore file and returns a Matcher
func Parse(src []byte) (Matcher, error) {
	matchers := []Matcher{}
	lines := bytes.Split(src, []byte{'\n'})
	for _, line := range lines {
		if len(line) == 0 || len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		if line[0] == '#' {
			continue
		}

		// TODO: line starts with '!'
		// TODO: line ends with '\ '

		// if starts with \# or \! then escaped
		if len(line) > 1 && line[0] == '\\' && (line[1] == '#' || line[1] == '!') {
			line = line[1:]
		}

		m, err := NewGlobMatch(line)
		if err != nil {
			return nil, err
		}
		matchers = append(matchers, m)
	}
	return NewMultiMatch(matchers), nil
}
