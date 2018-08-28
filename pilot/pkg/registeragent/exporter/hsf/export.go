package hsf

import (
	"encoding/json"
	"fmt"
	log "github.com/cihub/seelog"
	"github.com/gin-gonic/gin"
	"io/ioutil"
	"net/http"
	"strings"
)

type SimpleRpcInfoExporter struct {
}

func NewRpcInfoExporter() *SimpleRpcInfoExporter {
	s := &SimpleRpcInfoExporter{}
	return s
}

type ProviderInterface struct {
	InterfaceName string `json:"SERVICE_NAME"`
	Group         string `json:"GROUP"`
	Serialize     string `json:"SERIALIZE"`
}

type Result struct {
	Providers []ProviderInterface `json:"provider"`
}

type ProviderInterfaceDTO struct {
	Interface string `json:"interface"`
	Version   string `json:"version"`
	Group     string `json:"group"`
	Serialize string `json:"serialize"`
}
type InterfacesDTO struct {
	Providers []ProviderInterfaceDTO `json:"providers"`
	Protocol  string                 `json:"protocol"`
}

func (r *SimpleRpcInfoExporter) GetRpcServiceInfo(c *gin.Context) {
	localHost := "localhost"
	// default pandora qos port
	serverPort := 12201
	url := fmt.Sprintf("http://%v:%v/hsf/ls", localHost, serverPort)
	log.Infof("local rpc info url %v", url)
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Errorf("new local rpc request failed: ", err)
		return
	}
	req.Header.Add("Content-type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		log.Errorf("get local rpc info failed: ", err)
		return
	}
	defer resp.Body.Close()
	log.Infof("status: %v", string(resp.StatusCode))
	if resp.StatusCode == 200 {
		body, _ := ioutil.ReadAll(resp.Body)
		res := Result{}
		log.Infof("body string: %v", string(body))
		c.JSON(http.StatusOK, gin.H{"success": true, "data": res.unMarshal(body)})
	}
}

func (r *Result) unMarshal(data []byte) InterfacesDTO {
	if err := json.Unmarshal(data, &r); err != nil {
		log.Errorf("json unmarshal failed: %v", err)
	}
	interfacesDTO := InterfacesDTO{}
	interfacesDTO.Protocol = "HSF2.0"
	providers := r.Providers
	for i := range providers {
		fmt.Println(providers[i])
		uniqueInterfaceName := strings.Split(providers[i].InterfaceName, ":")
		if len(uniqueInterfaceName) != 2 {
			log.Errorf("interface name: %v format error: %v", providers[i].InterfaceName)
		}
		providerInterfaceDto := ProviderInterfaceDTO{uniqueInterfaceName[0], uniqueInterfaceName[1], providers[i].Group, providers[i].Serialize}
		interfacesDTO.Providers = append(interfacesDTO.Providers, providerInterfaceDto)
		fmt.Println(interfacesDTO.Providers)
	}
	return interfacesDTO
}
