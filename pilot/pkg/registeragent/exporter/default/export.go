package _default

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

type SimpleRpcInfoExporter struct {
}

func NewRpcInfoExporter() *SimpleRpcInfoExporter {
	s := &SimpleRpcInfoExporter{}
	return s
}

func (r *SimpleRpcInfoExporter) GetRpcServiceInfo(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": ""})
}
