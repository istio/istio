package spiffe


func WithIdentityDomain(value string, block func () ) {
	var savedIdentityDomain = globalIdentityDomain
	defer func() { globalIdentityDomain = savedIdentityDomain }()
	globalIdentityDomain = value
	block()
}

