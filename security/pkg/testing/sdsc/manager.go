package sdsc

import (
	"fmt"
)

// ClientManagerOptions specifies the options we create client manager.
type ClientManagerOptions struct {
	Count   int
	UDSPath string
}

// ClientManager manages the sds client.
type ClientManager struct {
	clients []*Client
}

// NewClientManager creates a bounch of sds testing client.
func NewClientManager(option ClientManagerOptions) (*ClientManager, error) {
	clients := []*Client{}
	for i := 0; i < option.Count; i++ {
		c, err := NewClient(ClientOptions{
			ServerAddress: option.UDSPath,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create %v client, error %v", i+1, err)
		}
		clients = append(clients, c)
	}
	return &ClientManager{
		clients: clients,
	}, nil
}
