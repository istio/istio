package plaintext

import (
	"bytes"
	"golang.org/x/net/html"
)

var blockTag = map[string]struct{}{
	"br":         {},
	"h1":         {},
	"h2":         {},
	"h3":         {},
	"h4":         {},
	"h5":         {},
	"pre":        {},
	"li":         {},
	"p":          {},
	"div":        {},
	"blockquote": {},
}

func isBlock(tag []byte) bool {
	_, ok := blockTag[string(tag)]
	return ok
}

// count number of newlines in a text block
func countNewlines(raw []byte) int {
	count := 0
	for idx := bytes.IndexByte(raw, '\n'); idx != -1 && idx < len(raw); raw = raw[idx:] {
		count++
		idx++
	}
	return count
}

// HTMLText extracts plain text from HTML markup
type HTMLText struct {
	InspectImageAlt bool
}

// InspectImageAlt is a sample for options  WIP
func InspectImageAlt(opt *HTMLText) error {
	opt.InspectImageAlt = true
	return nil
}

// NewHTMLText creates a new HTMLText extractor, using options.
func NewHTMLText(options ...func(*HTMLText) error) (*HTMLText, error) {
	extractor := HTMLText{}
	for _, option := range options {
		err := option(&extractor)
		if err != nil {
			return nil, err
		}
	}
	return &extractor, nil
}

// Text satisfies the plaintext.Extractor interface
func (p *HTMLText) Text(raw []byte) []byte {
	isCodeTag := false
	isStyleTag := false
	isScriptTag := false

	out := bytes.Buffer{}

	z := html.NewTokenizer(bytes.NewReader(raw))
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return out.Bytes()
		case html.StartTagToken:
			tn, hasAttr := z.TagName()
			if bytes.Equal(tn, []byte("code")) {
				isCodeTag = true
				continue
			}
			if bytes.Equal(tn, []byte("style")) {
				isStyleTag = true
				continue
			}
			if bytes.Equal(tn, []byte("script")) {
				isScriptTag = true
				continue
			}
			if bytes.Equal(tn, []byte("img")) {
				var key, val []byte
				for hasAttr {
					key, val, hasAttr = z.TagAttr()
					if len(val) > 0 && bytes.Equal(key, []byte("alt")) {
						out.Write(val)
						out.Write([]byte(" "))
					}
				}
			}
		case html.EndTagToken:
			tn, _ := z.TagName()
			if bytes.Equal(tn, []byte("code")) {
				isCodeTag = false
				continue
			}
			if bytes.Equal(tn, []byte("style")) {
				isStyleTag = false
				continue
			}
			if bytes.Equal(tn, []byte("script")) {
				isScriptTag = false
				continue
			}
		case html.TextToken:
			if isCodeTag || isStyleTag || isScriptTag {
				// we want to preserve the line count
				out.Write(bytes.Repeat([]byte{'\n'}, countNewlines(z.Text())))
				continue
			}
			out.Write([]byte(z.Text()))
		}
	}
}
