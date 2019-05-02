package queueinformer

import (
	"encoding/base64"
	"math/rand"
)

func NewLoopID() string {
	len := 5
	buff := make([]byte, len)
	rand.Read(buff)
	str := base64.StdEncoding.EncodeToString(buff)
	return str[:len]
}
