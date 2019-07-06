package nacos

import (
	"errors"
	"github.com/nacos-group/nacos-sdk-go/clients"
	"github.com/nacos-group/nacos-sdk-go/clients/naming_client"
	"github.com/nacos-group/nacos-sdk-go/common/constant"
	"strconv"
	"strings"
)

func CreateNacosServiceClient(addr string) (client *naming_client.INamingClient, err error) {
	if addr == "" {
		return nil, errors.New("输入的地址有误")
	}
	addrList := strings.Split(addr, ",")

	serverConfigs := make([]constant.ServerConfig, 0)

	for _, addrDetail := range addrList {
		var ip string
		var port uint64
		if strings.Contains(string(addrDetail), ":") {
			ip = strings.Split(string(addrDetail), ":")[0]
			var portError error
			port, portError = strconv.ParseUint(strings.Split(string(addrDetail), ":")[1], 10, 64)
			if portError != nil {
				return nil, errors.New("输入的端口有误")
			}
		} else {
			ip = string(addrDetail)
			port = DEFAULT_PORT
		}

		serverConfigs = append(serverConfigs, constant.ServerConfig{
			IpAddr:      ip,
			Port:        port,
			ContextPath: CONTEXT,
		})
	}

	clientConfig := constant.ClientConfig{
		TimeoutMs:      30 * 1000,
		ListenInterval: 10 * 1000,
		BeatInterval:   5 * 1000,
		LogDir:         "nacos/logs",
		CacheDir:       "nacos/cache",
	}

	namingClient, err := clients.CreateNamingClient(map[string]interface{}{
		"serverConfigs": serverConfigs,
		"clientConfig":  clientConfig,
	})
	return &namingClient, err
}

func (client *naming_client.INamingClient) addServiceChangedHandler() {

}
