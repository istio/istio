// Provides a ProtoSize method that can be swapped for proto.Size when using Google protos.

package internal

type hasSize interface {
	Size() int
}

func ProtoSize(msg hasSize) int {
	return msg.Size()
}
