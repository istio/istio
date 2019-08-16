package jws

func (s Signature) PublicHeaders() Headers {
	return s.headers
}

func (s Signature) ProtectedHeaders() Headers {
	return s.protected
}

func (s Signature) Signature() []byte {
	return s.signature
}

func (m Message) Payload() []byte {
	return m.payload
}

func (m Message) Signatures() []*Signature {
	return m.signatures
}

// LookupSignature looks up a particular signature entry using
// the `kid` value
func (m Message) LookupSignature(kid string) []*Signature {
	var sigs []*Signature
	for _, sig := range m.signatures {
		if hdr := sig.PublicHeaders(); hdr != nil {
			hdrKeyId, ok := hdr.Get(KeyIDKey)
			if ok && hdrKeyId == kid {
				sigs = append(sigs, sig)
				continue
			}
		}

		if hdr := sig.ProtectedHeaders(); hdr != nil {
			hdrKeyId, ok := hdr.Get(KeyIDKey)
			if ok && hdrKeyId == kid {
				sigs = append(sigs, sig)
				continue
			}
		}
	}
	return sigs
}
