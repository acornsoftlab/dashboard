package router

import (
	"github.com/acornsoftlab/dashboard/docs"
	model "github.com/acornsoftlab/dashboard/model/v1alpha1"
	"github.com/acornsoftlab/dashboard/pkg/app"
	"github.com/acornsoftlab/dashboard/pkg/lang"
	"github.com/acornsoftlab/dashboard/router/apis/_raw"
	api "github.com/acornsoftlab/dashboard/router/apis/clusters"
	"github.com/acornsoftlab/dashboard/router/apis/termapi"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	ginSwagger "github.com/swaggo/gin-swagger"
	"github.com/swaggo/gin-swagger/swaggerFiles"
	"net/http"
	"strings"
)

var Router *gin.Engine

func CreateUrlMappings() {

	// swagger docs
	docs.SwaggerInfo.Title = "kore-board API"
	docs.SwaggerInfo.Description = "mulit-cluster kubernetes dashboard api"
	docs.SwaggerInfo.Version = "1.0"
	docs.SwaggerInfo.Host = "github.com/acornsoftlab"
	docs.SwaggerInfo.BasePath = "/swegger"

	// gin
	Router = gin.Default()
	Router.Use(cors())         // cors
	Router.Use(route())        // route rules
	Router.Use(authenticate()) // authentication

	// Router.Use(gin.Logger())
	// Router.Use(Recovery())

	Router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler)) // restful-api docs
	Router.GET("/healthy", healthy)                                           // healthy
	Router.GET("/api/clusters", api.ListContexts)
	Router.POST("/api/clusters", api.CreateContexts)

	// Authentication API
	Router.POST("/api/token", api.Authentication) // authentication & get access token
	Router.DELETE("/api/token", api.Logout)

	// API
	clustersAPI := Router.Group("/api/clusters/:CLUSTER")
	{
		clustersAPI.GET("", api.GetContext)
		clustersAPI.POST("", api.CreateContext)
		clustersAPI.DELETE("", api.DeleteContext)
		clustersAPI.GET("/nodes/:NODE/metrics/:METRICS", api.GetNodeMetrics)                    // get node metrics
		clustersAPI.GET("/namespaces/:NAMESPACE/pods/:POD/metrics/:METRICS", api.GetPodMetrics) // get pod metrics
		clustersAPI.GET("/topology", api.Topology)                                              // get cluster topology graph
		clustersAPI.GET("/topology/namespaces/:NAMESPACE", api.Topology)                        // get namespace topology graph
		clustersAPI.GET("/dashboard", api.Dashboard)                                            // get dashboard
		//terminal API
		//terminal API
		clustersAPI.GET("/terminal", termapi.ProcCluster)
		clustersAPI.GET("namespaces/:NAMESPACE/pods/:POD/terminal", termapi.ProcPod)
		clustersAPI.GET("namespaces/:NAMESPACE/pods/:POD/containers/:CONTAINER/terminal", termapi.ProcContainer)
	}
	clustersAPI_ := Router.Group("/api")
	{
		clustersAPI_.GET("/topology", api.Topology)
		clustersAPI_.GET("/topology/namespaces/:NAMESPACE", api.Topology)
		clustersAPI_.GET("/dashboard", api.Dashboard)

		//for terminal websocket connect
		clustersAPI_.GET("/terminal/ws", termapi.GenerateHandleWS)
	}

	// RAW-API POST/PUT (apply, patch)
	Router.POST("/raw/clusters/:CLUSTER", _raw.ApplyRaw)
	Router.PUT("/raw/clusters/:CLUSTER", _raw.ApplyRaw)
	Router.POST("/raw", _raw.ApplyRaw)
	Router.PUT("/raw", _raw.ApplyRaw)

	// API-Group List
	Router.GET("/raw/clusters/:CLUSTER/apis/", _raw.GetAPIGroupList)
	Router.GET("/raw/apis/", _raw.GetAPIGroupList)

	// RAW-API Core
	//      non-Namespaced
	//          /api/v1/namespaces/kore
	//          /api/v1/nodes/apps-113
	//      Namespaced
	//          /api/v1/namespaces/default/services/kubernetes
	//          /api/v1/namespaces/default/serviceaccounts/default

	Router.GET("/raw/clusters/:CLUSTER/api/", _raw.GetRaw) // Core APIVersions
	rawAPI := Router.Group("/raw/clusters/:CLUSTER/api/:VERSION")
	{
		rawAPI.GET("", _raw.GetRaw)                             // ""                                       > core apiGroup - APIResourceLis
		rawAPI.GET("/:A", _raw.GetRaw)                          // "/:RESOURCE"                             > core apiGroup - list
		rawAPI.GET("/:A/:B", _raw.GetRaw)                       // "/:RESOURCE/:NAME"                       > core apiGroup - get
		rawAPI.DELETE("/:A/:B", _raw.DeleteRaw)                 // "/:RESOURCE/:NAME"                       > core apiGroup - delete
		rawAPI.PATCH("/:A/:B", _raw.PatchRaw)                   // "/:RESOURCE/:NAME"                       > core apiGroup - patch
		rawAPI.GET("/:A/:B/:RESOURCE", _raw.GetRaw)             // "/namespaces/:NAMESPACE/:RESOURCE"       > namespaced core apiGroup - list
		rawAPI.GET("/:A/:B/:RESOURCE/:NAME", _raw.GetRaw)       // "/namespaces/:NAMESPACE/:RESOURCE/:NAME" > namespaced core apiGroup - get
		rawAPI.DELETE("/:A/:B/:RESOURCE/:NAME", _raw.DeleteRaw) // "/namespaces/:NAMESPACE/:RESOURCE/:NAME" > namespaced core apiGroup - delete
		rawAPI.PATCH("/:A/:B/:RESOURCE/:NAME", _raw.PatchRaw)   // "/namespaces/:NAMESPACE/:RESOURCE/:NAME" > namespaced core apiGroup - patch
	}
	Router.GET("/raw/api/", _raw.GetRaw) // Core APIVersions
	rawAPI_ := Router.Group("/raw/api/:VERSION")
	{
		rawAPI_.GET("", _raw.GetRaw)                             // ""                                       > core apiGroup - APIResourceList
		rawAPI_.GET("/:A", _raw.GetRaw)                          // "/:RESOURCE"                             > core apiGroup - list
		rawAPI_.GET("/:A/:B", _raw.GetRaw)                       // "/:RESOURCE/:NAME"                       > core apiGroup - get
		rawAPI_.DELETE("/:A/:B", _raw.DeleteRaw)                 // "/:RESOURCE/:NAME"                       > core apiGroup - delete
		rawAPI_.PATCH("/:A/:B", _raw.PatchRaw)                   // "/:RESOURCE/:NAME"                       > core apiGroup - patch
		rawAPI_.GET("/:A/:B/:RESOURCE", _raw.GetRaw)             // "/namespaces/:NAMESPACE/:RESOURCE"       > namespaced core apiGroup - list
		rawAPI_.GET("/:A/:B/:RESOURCE/:NAME", _raw.GetRaw)       // "/namespaces/:NAMESPACE/:RESOURCE/:NAME" > namespaced core apiGroup - get
		rawAPI_.DELETE("/:A/:B/:RESOURCE/:NAME", _raw.DeleteRaw) // "/namespaces/:NAMESPACE/:RESOURCE/:NAME" > namespaced core apiGroup - delete
		rawAPI_.PATCH("/:A/:B/:RESOURCE/:NAME", _raw.PatchRaw)   // "/namespaces/:NAMESPACE/:RESOURCE/:NAME" > namespaced core apiGroup - patch
	}

	// RAW-API Grouped
	//      non-Namespaced
	//          /apis/metrics.k8s.io/v1beta1/nodes/apps-115
	//      Namespaced
	//          /apis/apps/v1/namespaces/kube-system/deployments/nginx
	//          /apis/rbac.authorization.k8s.io/v1/namespaces/default/rolebindings/clusterrolebinding-2g782
	Router.GET("/raw/clusters/:CLUSTER/apis/:GROUP", _raw.GetRaw) // APIGroup
	rawAPIs := Router.Group("/raw/clusters/:CLUSTER/apis/:GROUP/:VERSION")
	{
		rawAPIs.GET("", _raw.GetRaw)                             // ""                                          > apiGroup - APIResourceList
		rawAPIs.GET("/:A", _raw.GetRaw)                          // "/:RESOURCE"                                > apiGroup - list
		rawAPIs.GET("/:A/:B", _raw.GetRaw)                       // "/:RESOURCE/:NAME"                          > apiGroup - get
		rawAPIs.DELETE("/:A/:B", _raw.DeleteRaw)                 // "/:RESOURCE/:NAME"                          > apiGroup - delete
		rawAPIs.PATCH("/:A/:B", _raw.PatchRaw)                   // "/:RESOURCE/:NAME"                          > apiGroup - patch
		rawAPIs.GET("/:A/:B/:RESOURCE", _raw.GetRaw)             // "/namespaces/:NAMESPACE/:RESOURCE"          > namespaced apiGroup - list
		rawAPIs.GET("/:A/:B/:RESOURCE/:NAME", _raw.GetRaw)       // "/namespaces/:NAMESPACE/:RESOURCE/:NAME"    > namespaced apiGroup - get
		rawAPIs.DELETE("/:A/:B/:RESOURCE/:NAME", _raw.DeleteRaw) // "/namespaces/:NAMESPACE/:RESOURCE/:NAME"    > namespaced apiGroup - delete
		rawAPIs.PATCH("/:A/:B/:RESOURCE/:NAME", _raw.PatchRaw)   // "/namespaces/:NAMESPACE/:RESOURCE/:NAME"    > namespaced apiGroup - patch
	}
	Router.GET("/raw/apis/:GROUP", _raw.GetRaw) // APIGroup
	rawAPIs_ := Router.Group("/raw/apis/:GROUP/:VERSION")
	{
		rawAPIs_.GET("", _raw.GetRaw)                             // ""                                          > apiGroup - APIResourceList
		rawAPIs_.GET("/:A", _raw.GetRaw)                          // "/:RESOURCE"                                > apiGroup - list
		rawAPIs_.GET("/:A/:B", _raw.GetRaw)                       // "/:RESOURCE/:NAME"                          > apiGroup - get
		rawAPIs_.DELETE("/:A/:B", _raw.DeleteRaw)                 // "/:RESOURCE/:NAME"                          > apiGroup - delete
		rawAPIs_.PATCH("/:A/:B", _raw.PatchRaw)                   // "/:RESOURCE/:NAME"                          > apiGroup - patch
		rawAPIs_.GET("/:A/:B/:RESOURCE", _raw.GetRaw)             // "/namespaces/:NAMESPACE/:RESOURCE"          > namespaced apiGroup - list
		rawAPIs_.GET("/:A/:B/:RESOURCE/:NAME", _raw.GetRaw)       // "/namespaces/:NAMESPACE/:RESOURCE/:NAME"    > namespaced apiGroup - get
		rawAPIs_.DELETE("/:A/:B/:RESOURCE/:NAME", _raw.DeleteRaw) // "/namespaces/:NAMESPACE/:RESOURCE/:NAME"    > namespaced apiGroup - delete
		rawAPIs_.PATCH("/:A/:B/:RESOURCE/:NAME", _raw.PatchRaw)   // "/namespaces/:NAMESPACE/:RESOURCE/:NAME"    > namespaced apiGroup - patch
	}

}

/**
  RAW-API  URL Route resolved handler
*/
func route() gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.HasPrefix(c.Request.RequestURI, "/raw/clusters") || strings.HasPrefix(c.Request.RequestURI, "/raw/api") {
			if c.Param("RESOURCE") == "" {
				c.Params = append(c.Params,
					gin.Param{Key: "RESOURCE", Value: c.Param("A")},
					gin.Param{Key: "NAME", Value: c.Param("B")})
			} else if c.Param("A") == "namespaces" {
				c.Params = append(c.Params, gin.Param{Key: "NAMESPACE", Value: c.Param("B")})
			}
		}
	}
}

func cors() gin.HandlerFunc {

	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", lang.NVL(c.Request.Header.Get("Origin"), "*"))
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

func authenticate() gin.HandlerFunc {

	return func(c *gin.Context) {

		if !lang.ArrayContains([]string{"/healthy", "/api/token"}, c.Request.RequestURI) {
			token, _ := c.Cookie("access-token")
			expired := true
			if token != "" {
				expired, _ = model.ValidateAccessToken(token)
			}
			// expired "access-token"
			if expired {
				token, _ := c.Cookie("refresh-token")
				if token == "" {
					log.Infoln("expired access-token, refresh-token")
					c.AbortWithStatus(http.StatusUnauthorized)
					return
				}
				expired, err := model.ValidateRefreshToken(token)
				if err != nil || expired {
					log.Infoln("expired refresh-token")
					c.AbortWithStatus(http.StatusUnauthorized)
					return
				}
				//create a token
				model.CreateToken(c)
				log.Infoln("create a new token(access, refresh) from refresh-token")
			}

		}

		c.Next()
	}
}

func healthy(c *gin.Context) {
	g := app.Gin{C: c}
	g.Send(http.StatusOK, "healthy")
}
