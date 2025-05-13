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
	usersGroup := parent.Group("/user").Use(s.jwtBearerMiddleware)
	usersGroup.POST("/load", s.getUserInfo)
	usersGroup.POST("/update", s.updateUser)
}

func (s *Server) setupFrontendRoutes(parent *gin.RouterGroup) {
	frontendGroup := parent.Group("")
	frontendGroup.GET("", s.handleFrontendLanding)
	frontendGroup.GET("/login", s.handleFrontendLogin)
	frontendGroup.GET("/dashboard", s.handleFrontendDashboard)
}
