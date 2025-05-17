package main

import (
	"github.com/gin-gonic/gin"
)

func (s *Server) handleFrontendLanding(c *gin.Context) {
	writeTemplate(c, "landing", nil)
}

func (s *Server) handleFrontendLogin(c *gin.Context) {
	writeTemplate(c, "login", nil)
}

func (s *Server) handleFrontendDashboard(c *gin.Context) {
	writeTemplate(c, "dashboard", nil)
}

func (s *Server) handleFrontendUserEditor(c *gin.Context) {
	writeTemplate(c, "usereditor", nil)
}

func (s *Server) handleFrontendConfigEditor(c *gin.Context) {
	writeTemplate(c, "configeditor", nil)
}
