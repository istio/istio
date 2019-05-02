package listtype

// +k8s:openapi-gen=true
type Item struct {
	Port     int
	Protocol string
}

// +k8s:openapi-gen=true
type MapList struct {
	// +listType=map
	// +listMapKey=port
	Field []Item
}
