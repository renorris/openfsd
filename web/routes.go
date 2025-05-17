package main

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"os"
)

func (s *Server) setupRoutes() (e *gin.Engine) {
	e = gin.New()
	e.Use(gin.Recovery())
	if os.Getenv("GIN_LOGGER") != "" {
		e.Use(gin.Logger())
	}

	e.POST("/j", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/api/v1/fsd-jwt")
	})

	// API groups
	apiV1Group := e.Group("/api/v1")
	apiV1Group.POST("/fsd-jwt", s.getFsdJwt)
	s.setupAuthRoutes(apiV1Group)
	s.setupUserRoutes(apiV1Group)
	s.setupConfigRoutes(apiV1Group)

	// Frontend groups
	s.setupFrontendRoutes(e.Group(""))

	// Serve static files
	e.Static("/static", "./static")

	return
}

func (s *Server) setupAuthRoutes(parent *gin.RouterGroup) {
	authGroup := parent.Group("/auth")
	authGroup.POST("/login", s.getAccessRefreshTokens)
	authGroup.POST("/refresh", s.refreshAccessToken)
}

func (s *Server) setupUserRoutes(parent *gin.RouterGroup) {
	usersGroup := parent.Group("/user")
	usersGroup.Use(s.jwtBearerMiddleware)
	usersGroup.POST("/load", s.getUserByCID)
	usersGroup.PATCH("/update", s.updateUser)
	usersGroup.POST("/create", s.createUser)
}

func (s *Server) setupConfigRoutes(parent *gin.RouterGroup) {
	configGroup := parent.Group("/config")
	configGroup.Use(s.jwtBearerMiddleware)
	configGroup.GET("/load", s.handleGetConfig)
	configGroup.POST("/update", s.handleUpdateConfig)
	configGroup.POST("/resetsecretkey", s.handleResetSecretKey)
	configGroup.POST("/createtoken", s.handleCreateNewAPIToken)
}

func (s *Server) setupFrontendRoutes(parent *gin.RouterGroup) {
	frontendGroup := parent.Group("")
	frontendGroup.GET("", s.handleFrontendLanding)
	frontendGroup.GET("/login", s.handleFrontendLogin)
	frontendGroup.GET("/dashboard", s.handleFrontendDashboard)
	frontendGroup.GET("/usereditor", s.handleFrontendUserEditor)
	frontendGroup.GET("/configeditor", s.handleFrontendConfigEditor)
}
