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
	// Production default: release (no [GIN-debug] noise). Honor GIN_MODE and
	// leave TestMode alone for unit tests that call gin.SetMode(gin.TestMode).
	if os.Getenv(gin.EnvGinMode) == "" && gin.Mode() != gin.TestMode {
		gin.SetMode(gin.ReleaseMode)
	}

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

	// Version-agnostic public discovery + OpenAPI (no JWT, no version-reject).
	apiV1Group.GET("", s.handleAPIDiscovery)
	apiV1Group.GET("/versions", s.handleAPIDiscovery)
	apiV1Group.GET("/openapi.json", s.handleOpenAPIJSON)
	apiV1Group.GET("/openapi.yaml", s.handleOpenAPIYAML)

	apiV1Group.POST("/fsd-jwt", s.getFsdJwt) // outside microversion reject
	s.setupAuthRoutes(apiV1Group)            // login/refresh: soft version headers only
	s.setupDataRoutes(apiV1Group)            // never version-reject

	// Dual-accept JSON resource groups (jwt + csrf + apiVersion + bearer revalidate).
	s.setupUserRoutes(apiV1Group)
	s.setupConfigRoutes(apiV1Group)
	s.setupFsdConnRoutes(apiV1Group)
	s.setupSweatboxAPIRoutes(apiV1Group)
	s.setupEditorAPIRoutes(apiV1Group)
	s.setupAccountAPIRoutes(apiV1Group)

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
	// Legacy RPC-ish paths (Stable; preserved).
	userRPC := parent.Group("/user")
	s.useAPIV1Protected(userRPC)
	userRPC.POST("/load", s.getUserByCID)
	userRPC.PATCH("/update", s.updateUser)
	userRPC.POST("/create", s.createUser)

	// Resource-oriented directory (Stable) — SUP+ list; self or SUP+ get.
	// See docs/design/rest-api-versioning.md §B.
	usersDir := parent.Group("/users")
	s.useAPIV1Protected(usersDir)
	usersDir.GET("", s.handleAPIListUsers)
	usersDir.GET("/:cid", s.handleAPIGetUser)
}

func (s *Server) setupConfigRoutes(parent *gin.RouterGroup) {
	configGroup := parent.Group("/config")
	s.useAPIV1Protected(configGroup)
	configGroup.GET("/load", s.handleGetConfig)
	configGroup.POST("/update", s.handleUpdateConfig)
	configGroup.POST("/resetsecretkey", s.handleResetSecretKey)
	configGroup.POST("/createtoken", s.handleCreateNewAPIToken)
}

func (s *Server) setupFsdConnRoutes(parent *gin.RouterGroup) {
	fsdConnGroup := parent.Group("/fsdconn")
	s.useAPIV1Protected(fsdConnGroup)
	fsdConnGroup.POST("/kickuser", s.handleKickActiveConnection)
}

// setupSweatboxAPIRoutes mounts /api/v1/sweatbox (Instructor1+ dual-accept).
//
// Raw GET /state and /ops stay non-envelope for PE polls. Mutations and GET
// /session use the APIV1 envelope with the design §D FSD status mapping.
// HTML form POSTs under /sweatbox/* remain for the MPA (CSRF + PRG).
func (s *Server) setupSweatboxAPIRoutes(parent *gin.RouterGroup) {
	g := parent.Group("/sweatbox")
	s.useAPIV1Protected(g)

	// Raw PE reads (non-envelope)
	g.GET("/state", s.handleAPISweatboxState)
	g.GET("/ops", s.handleAPISweatboxOps)

	// Enveloped operator surface (Stable; design §D)
	g.GET("/session", s.handleAPISweatboxSession)
	g.POST("/airport", s.handleAPISweatboxAirport)
	g.POST("/scenario", s.handleAPISweatboxScenario)
	g.POST("/command", s.handleAPISweatboxCommand)
	g.POST("/pause", s.handleAPISweatboxPause)
	g.POST("/unpause", s.handleAPISweatboxUnpause)
	g.DELETE("/aircraft/:callsign", s.handleAPISweatboxDeleteAircraft)
	g.DELETE("/aircraft", s.handleAPISweatboxDeleteAllAircraft)
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

	// Account self-service — any DB-valid session (OBS+).
	authed.GET("/account", s.handleFrontendAccount)
	authed.POST("/account/password", s.handleFrontendAccountPassword)
	authed.POST("/account/delete", s.handleFrontendAccountDelete)

	// User editor — Supervisor+ (create + full profile + ratings).
	userAdmin := authed.Group("")
	userAdmin.Use(s.requireMinRatingHTML(protocol.NetworkRatingSupervisor))
	userAdmin.GET("/usereditor", s.handleFrontendUserEditor)
	userAdmin.POST("/usereditor/create", s.handleFrontendUserCreate)
	userAdmin.POST("/usereditor/update", s.handleFrontendUserUpdate)

	// Instructor1+ tools: sweatbox + airport editor (HTML forms; CSRF on mutations).
	instructor := authed.Group("")
	instructor.Use(s.requireMinRatingHTML(protocol.NetworkRatingInstructor1))
	instructor.GET("/sweatbox", s.handleFrontendSweatbox)
	instructor.GET("/sweatbox/manual", s.handleFrontendSweatboxManual)
	instructor.POST("/sweatbox/airport", s.handleFrontendSweatboxAirport)
	instructor.POST("/sweatbox/scenario", s.handleFrontendSweatboxScenario)
	instructor.POST("/sweatbox/command", s.handleFrontendSweatboxCommand)
	instructor.POST("/sweatbox/pause", s.handleFrontendSweatboxPause)
	instructor.POST("/sweatbox/unpause", s.handleFrontendSweatboxUnpause)
	instructor.POST("/sweatbox/delete", s.handleFrontendSweatboxDelete)
	instructor.POST("/sweatbox/delete-all", s.handleFrontendSweatboxDeleteAll)

	// Airport editor: HTML shell + echo-download (no disk/DB persistence).
	instructor.GET("/airport-editor", s.handleFrontendAirportEditor)
	instructor.POST("/airport-editor/download-apt", s.handleFrontendAirportEditorDownloadAPT)
	instructor.POST("/airport-editor/download-air", s.handleFrontendAirportEditorDownloadAIR)

	// Admin config only: form POST mutations with CSRF; no JS required.
	admin := authed.Group("")
	admin.Use(s.requireMinRatingHTML(protocol.NetworkRatingAdministator))
	admin.GET("/configeditor", s.handleFrontendConfigEditor)
	admin.POST("/configeditor", s.handleFrontendConfigUpdate)
	admin.POST("/configeditor/reset-secret", s.handleFrontendConfigResetSecret)
	admin.POST("/configeditor/create-token", s.handleFrontendConfigCreateToken)
}
