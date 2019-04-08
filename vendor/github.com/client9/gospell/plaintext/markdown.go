package plaintext

import (
	"bytes"
	"regexp"
)

var allSymbols = regexp.MustCompile("^[ =*|-]*$")
var linkTarget = regexp.MustCompile(`\]\([^ )]*\)?`)
var blockQuote = regexp.MustCompile("^>[ >]*")
var leadingHeadline = regexp.MustCompile("^ *#+ *")
var trailingHeadline = regexp.MustCompile(" *#+ *$")

// code fences can have leading whitespace apparently
var codeFence = regexp.MustCompile("^\\s*```")

// single line, single back quote code snippet
// this is the most common case although markdown
// apparently supports ``...`\n\n....`` style multi-line
// to allow embedded back quotes
var simpleCode = regexp.MustCompile("`[^`]+`")

// MarkdownText extracts plain text from markdown sources
type MarkdownText struct {
	Extractor Extractor
}

// NewMarkdownText creates a new extractor
func NewMarkdownText(options ...func(*MarkdownText) error) (*MarkdownText, error) {
	processor := MarkdownText{}
	for _, option := range options {
		err := option(&processor)
		if err != nil {
			return nil, err
		}
	}

	if processor.Extractor == nil {
		e, err := NewHTMLText()
		if err != nil {
			return nil, err
		}
		processor.Extractor = e
	}

	return &processor, nil
}

func cleanupLine(s []byte) []byte {

	// strip away various headings from back and front
	s = leadingHeadline.ReplaceAll(s, nil)
	s = trailingHeadline.ReplaceAll(s, nil)

	// strip away leading "> > > " from block quotes
	s = blockQuote.ReplaceAll(s, nil)

	// is all "-", "=", "*", "|" make empty
	// this eliminates various HR variations and
	// table decoration and is not a word anyways
	if allSymbols.Match(s) {
		return []byte{}
	}

	s = simpleCode.ReplaceAll(s, nil)

	// there is no reason to NOT replace `*` `~` or `_` with a space character
	// not used in words
	s = bytes.Replace(s, []byte{'*'}, nil, -1)
	s = bytes.Replace(s, []byte{'~'}, nil, -1)
	s = bytes.Replace(s, []byte{'_'}, nil, -1)

	// links. 	[link](/MyURI)
	// Stuff inside the "link" can be on different lines, but "](/URI)"
	// is all on one line so we can delete ](....space )
	// ![ is for images
	s = bytes.Replace(s, []byte{'!', '['}, nil, -1)
	s = bytes.Replace(s, []byte{'['}, nil, -1)
	s = linkTarget.ReplaceAll(s, nil)
	return s
}

// Text extracts text from a markdown source
func (p *MarkdownText) Text(text []byte) []byte {
	inCodeFence := false
	inCodeIndent := false

	buf := bytes.Buffer{}
	lines := bytes.Split(text, []byte{'\n'})
	for pos, line := range lines {
		if pos > 0 {
			buf.WriteByte('\n')
		}

		if codeFence.Match(line) {
			inCodeFence = !inCodeFence
			continue
		}

		if bytes.HasPrefix(line, []byte{' ', ' ', ' ', ' '}) {
			inCodeIndent = !inCodeIndent
			continue
		}

		if !inCodeFence && !inCodeIndent {
			buf.Write(cleanupLine(line))
		}
	}
	return p.Extractor.Text(buf.Bytes())
}
