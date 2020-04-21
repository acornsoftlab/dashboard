package apis

import (
	"net/http"

	"github.com/acornsoftlab/kore3/pkg/app"
	"github.com/acornsoftlab/kore3/pkg/config"
	"github.com/gin-gonic/gin"
)

func ListContexts(c *gin.Context) {
	g := app.Gin{C: c}

	ctx := map[string]interface{}{
		"currentContext": config.Value.CurrentContext,
		"contexts":       config.Value.Contexts,
	}

	g.Send(http.StatusOK, ctx)
}