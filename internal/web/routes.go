package web

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/pkg/protocol"
)

//go:embed static/*
var staticFS embed.FS

func (s *Server) setupRoutes() (*gin.Engine, error) {
	e := gin.New()
	e.Use(gin.Recovery())
	if os.Getenv("GIN_LOGGER") != "" {
		e.Use(gin.Logger())
	}

	e.POST("/j", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/api/v1/fsd-jwt")
	})

	// API groups — dual-accept Bearer | session cookie; CSRF when cookie-authenticated.
	apiV1Group := e.Group("/api/v1")
	apiV1Group.POST("/fsd-jwt", s.getFsdJwt)
	s.setupAuthRoutes(apiV1Group)
	s.setupUserRoutes(apiV1Group)
	s.setupConfigRoutes(apiV1Group)
	s.setupDataRoutes(apiV1Group)
	s.setupFsdConnRoutes(apiV1Group)

	// Frontend groups
	s.setupFrontendRoutes(e.Group(""))

	// Serve static files
	subFS, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, fmt.Errorf("static subfs: %w", err)
	}
	e.StaticFS("/static", http.FS(subFS))

	return e, nil
}

func (s *Server) setupAuthRoutes(parent *gin.RouterGroup) {
	authGroup := parent.Group("/auth")
	authGroup.POST("/login", s.getAccessRefreshTokens)
	authGroup.POST("/refresh", s.refreshAccessToken)
}

func (s *Server) setupUserRoutes(parent *gin.RouterGroup) {
	usersGroup := parent.Group("/user")
	usersGroup.Use(s.jwtBearerMiddleware, s.csrfIfCookieSession)
	usersGroup.POST("/load", s.getUserByCID)
	usersGroup.PATCH("/update", s.updateUser)
	usersGroup.POST("/create", s.createUser)
}

func (s *Server) setupConfigRoutes(parent *gin.RouterGroup) {
	configGroup := parent.Group("/config")
	configGroup.Use(s.jwtBearerMiddleware, s.csrfIfCookieSession)
	configGroup.GET("/load", s.handleGetConfig)
	configGroup.POST("/update", s.handleUpdateConfig)
	configGroup.POST("/resetsecretkey", s.handleResetSecretKey)
	configGroup.POST("/createtoken", s.handleCreateNewAPIToken)
}

func (s *Server) setupFsdConnRoutes(parent *gin.RouterGroup) {
	fsdConnGroup := parent.Group("/fsdconn")
	fsdConnGroup.Use(s.jwtBearerMiddleware, s.csrfIfCookieSession)
	fsdConnGroup.POST("/kickuser", s.handleKickActiveConnection)
}

func (s *Server) setupDataRoutes(parent *gin.RouterGroup) {
	dataGroup := parent.Group("/data")
	dataGroup.GET("/status.txt", s.handleGetStatusTxt)
	dataGroup.GET("/status.json", s.handleGetStatusJSON)
	dataGroup.GET("/openfsd-servers.txt", s.handleGetServersTxt)
	dataGroup.GET("/openfsd-servers.json", s.handleGetServersJSON)
	dataGroup.GET("/sweatbox-servers.json", func(c *gin.Context) {
		c.Set("is_sweatbox", "true")
		s.handleGetServersJSON(c)
	})
	dataGroup.GET("/all-servers.json", s.handleGetServersJSON)
	dataGroup.GET("/openfsd-data.json", s.getDatafeed)
}

func (s *Server) setupFrontendRoutes(parent *gin.RouterGroup) {
	frontendGroup := parent.Group("")
	frontendGroup.GET("", s.handleFrontendLanding)
	frontendGroup.GET("/login", s.handleFrontendLogin)
	frontendGroup.POST("/login", s.handleFrontendLoginPost)
	frontendGroup.POST("/logout", s.handleFrontendLogoutPost)

	authed := frontendGroup.Group("")
	authed.Use(s.requireSessionHTML)
	authed.GET("/dashboard", s.handleFrontendDashboard)

	sup := authed.Group("")
	sup.Use(s.requireMinRatingHTML(protocol.NetworkRatingSupervisor))
	sup.GET("/usereditor", s.handleFrontendUserEditor)

	admin := authed.Group("")
	admin.Use(s.requireMinRatingHTML(protocol.NetworkRatingAdministator))
	admin.GET("/configeditor", s.handleFrontendConfigEditor)
}
