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
