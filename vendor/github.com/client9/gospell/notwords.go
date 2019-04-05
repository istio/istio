package gospell

import (
	"bytes"
	"strings"
)

// Functions to remove non-words such as URLs, file paths, etc.

// This needs auditing as I believe it is wrong
func enURLChar(c rune) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '-' ||
		c == '_' ||
		c == '\\' ||
		c == '.' ||
		c == ':' ||
		c == ';' ||
		c == '/' ||
		c == '~' ||
		c == '%' ||
		c == '*' ||
		c == '$' ||
		c == '[' ||
		c == ']' ||
		c == '?' ||
		c == '#' ||
		c == '!'
}
func enNotURLChar(c rune) bool {
	return !enURLChar(c)
}

// RemoveURL attempts to strip away obvious URLs
//
func RemoveURL(s string) string {
	var idx int

	for {
		if idx = strings.Index(s, "http"); idx == -1 {
			return s
		}

		news := s[:idx]
		endx := strings.IndexFunc(s[idx:], enNotURLChar)
		if endx != -1 {
			news = news + " " + s[idx+endx:]
		}
		s = news
	}
}

// RemovePath attempts to strip away embedded file system paths, e.g.
//  /foo/bar or /static/myimg.png
//
//  TODO: windows style
//
func RemovePath(s string) string {
	out := bytes.Buffer{}
	var idx int
	for len(s) > 0 {
		if idx = strings.IndexByte(s, '/'); idx == -1 {
			out.WriteString(s)
			break
		}

		if idx > 0 {
			idx--
		}

		var chclass string
		switch s[idx] {
		case '/', ' ', '\n', '\t', '\r':
			chclass = " \n\r\t"
		case '[':
			chclass = "]\n"
		case '(':
			chclass = ")\n"
		default:
			out.WriteString(s[:idx+2])
			s = s[idx+2:]
			continue
		}

		endx := strings.IndexAny(s[idx+1:], chclass)
		if endx != -1 {
			out.WriteString(s[:idx+1])
			out.Write(bytes.Repeat([]byte{' '}, endx))
			s = s[idx+endx+1:]
		} else {
			out.WriteString(s)
			break
		}
	}
	return out.String()
}
