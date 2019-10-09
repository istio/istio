// GENERATED FILE -- DO NOT EDIT
//

package basicmeta

import (
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
)

var (

	// Collection1 is the name of collection collection1
	Collection1 = collection.NewName("collection1")

	// Collection2 is the name of collection collection2
	Collection2 = collection.NewName("collection2")
)

// CollectionNames returns the collection names declared in this package.
func CollectionNames() []collection.Name {
	return []collection.Name{
		Collection1,
		Collection2,
	}
}
