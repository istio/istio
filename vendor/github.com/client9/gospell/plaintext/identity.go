package plaintext

// Identity provides a pass-through plain text extractor
type Identity struct {
}

// NewIdentity creates an identity-extractor
func NewIdentity() (*Identity, error) {
	return &Identity{}, nil
}

// Text satisfies the plaintext.Extractor interface
func (p *Identity) Text(raw []byte) []byte {
	return raw
}
