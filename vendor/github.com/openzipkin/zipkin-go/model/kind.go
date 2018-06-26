package model

// Kind clarifies context of timestamp, duration and remoteEndpoint in a span.
type Kind string

// Available Kind values
const (
	Undetermined Kind = ""
	Client       Kind = "CLIENT"
	Server       Kind = "SERVER"
	Producer     Kind = "PRODUCER"
	Consumer     Kind = "CONSUMER"
)
