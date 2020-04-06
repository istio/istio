package featurelabels

type Security struct {
	MTLS MTLS
}

type MTLS struct {
}

type Observability struct {
	Status Status
}

type Status struct{}

type Usability struct {
	Observability Observability
}
