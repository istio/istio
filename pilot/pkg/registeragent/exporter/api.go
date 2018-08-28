package exporter

import (
	"github.com/gin-gonic/gin"
)

type RpcAcutator interface {
	GetRpcServiceInfo(c *gin.Context)
}
