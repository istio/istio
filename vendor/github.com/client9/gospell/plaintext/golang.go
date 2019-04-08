package plaintext

import (
	"bytes"
	"text/scanner"
)

// GolangText extracts plaintext from Golang and other similar C or Java like files
//
// Need to study.   https://godoc.org/github.com/fluhus/godoc-tricks
//  Does not process embedded code blocks
//
type GolangText struct {
}

// NewGolangText creates a new extractor
func NewGolangText() (*GolangText, error) {
	return &GolangText{}, nil
}

// Text satisfies the Extractor interface
//
//ReplaceGo is a specialized routine for correcting Golang source
// files.  Currently only checks comments, not identifiers for
// spelling.
//
// Other items:
//   - check strings, but need to ignore
//      * import "statements" blocks
//      * import ( "blocks" )
//   - skip first comment (line 0) if build comment
//
func (p *GolangText) Text(raw []byte) []byte {
	out := bytes.Buffer{}
	s := scanner.Scanner{}
	s.Init(bytes.NewReader(raw))
	s.Error = (func(s *scanner.Scanner, msg string) {})
	s.Mode = scanner.ScanIdents | scanner.ScanFloats | scanner.ScanChars | scanner.ScanStrings | scanner.ScanRawStrings | scanner.ScanComments
	for {
		switch s.Scan() {
		case scanner.Comment:
			out.WriteString(s.TokenText())
			out.WriteByte('\n')
		case scanner.EOF:
			return out.Bytes()
		}
	}
}
