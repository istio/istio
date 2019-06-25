package node

const (
	// Max burst values for ratelimiting
	// Requests containing more than this number of
	// operations will always be rejected
	AttestLimit int = 1
	CSRLimit    int = 500
	JSRLimit    int = 500
)
