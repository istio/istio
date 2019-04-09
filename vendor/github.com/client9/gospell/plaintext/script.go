package plaintext

import (
	"bytes"
)

// ScriptText extract plaintext from "generic script" languages
// that use the '#' character to denote a comment line
// It's not so smart.
// TODO: add support for Ruby, multi-line comment
//  http://www.tutorialspoint.com/ruby/ruby_comments.htm
type ScriptText struct {
}

// NewScriptText creates a new file extractor
func NewScriptText() (*ScriptText, error) {
	return &ScriptText{}, nil
}

// Text extracts plaintext
func (p *ScriptText) Text(text []byte) []byte {
	buf := bytes.Buffer{}
	lines := bytes.Split(text, []byte{'\n'})
	for pos, line := range lines {
		if pos > 0 {
			buf.WriteByte('\n')
		}

		// BUG: if '#' is in a string
		if idx := bytes.IndexByte(line, '#'); idx != -1 {
			buf.Write(line[idx:])
		}
	}
	return buf.Bytes()
}
