package john

import (
	"encoding/json"
	"fmt"
)

type Map map[string]any

func (m Map) JSON() string {
	b, err := json.Marshal(m)
	if err != nil {
		panic(fmt.Sprintf("json Marshal: %v", err))
	}
	return string(b)
}