package plaintext

// Extractor is an interface for extracting plaintext
type Extractor interface {
	Text([]byte) []byte
}
