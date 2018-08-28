package hsf

import (
	"github.com/gin-gonic/gin"
)

type SimpleRpcInfoExporter struct {
}

func NewRpcInfoExporter() *SimpleRpcInfoExporter {
	s := &SimpleRpcInfoExporter{}
	return s
}

func (r *SimpleRpcInfoExporter) GetRpcServiceInfo(c *gin.Context) {

}
