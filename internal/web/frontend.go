package web

import (
	"github.com/gin-gonic/gin"
)

func (s *Server) handleFrontendLanding(c *gin.Context) {
	s.writeTemplate(c, "landing", nil)
}

func (s *Server) handleFrontendLogin(c *gin.Context) {
	s.writeTemplate(c, "login", nil)
}

func (s *Server) handleFrontendDashboard(c *gin.Context) {
	s.writeTemplate(c, "dashboard", nil)
}

func (s *Server) handleFrontendUserEditor(c *gin.Context) {
	s.writeTemplate(c, "usereditor", nil)
}

func (s *Server) handleFrontendConfigEditor(c *gin.Context) {
	s.writeTemplate(c, "configeditor", nil)
}
