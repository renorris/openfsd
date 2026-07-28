package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/pkg/protocol"
)

func (s *Server) handleFrontendLanding(c *gin.Context) {
	claims := s.optionalSession(c)
	s.writeTemplate(c, "landing", basePage{
		User:      pageUserFromClaims(claims),
		CSRFToken: s.issueCSRFToken(c),
	})
}

func (s *Server) handleFrontendLogin(c *gin.Context) {
	// Already signed in → dashboard.
	// Delete path clears cookies first so the account-deleted banner is reachable.
	if claims, err := s.parseSessionCookie(c); err == nil && claims != nil {
		// Still revalidate: inactive sessions must not bounce to dashboard.
		if _, _, err := s.revalidateSessionFromDB(claims); err == nil {
			c.Redirect(http.StatusSeeOther, "/dashboard")
			return
		}
		s.clearSessionCookie(c)
		s.clearCSRFCookie(c)
	}
	page := loginPage{
		basePage: basePage{
			CSRFToken: s.issueCSRFToken(c),
		},
	}
	if c.Query("account") == "deleted" {
		page.Info = "Your account has been deleted."
		if c.Query("permanent") == "disabled" {
			page.Info += " Permanent delete is not enabled on this server; the account was deactivated instead."
		}
	}
	s.writeTemplate(c, "login", page)
}

// handleFrontendLoginPost processes application/x-www-form-urlencoded login
// without requiring JavaScript. Success: Set-Cookie + 303 /dashboard.
// Failure: re-render form with errors near fields (200).
func (s *Server) handleFrontendLoginPost(c *gin.Context) {
	if !s.validateCSRF(c) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	cidStr := strings.TrimSpace(c.PostForm("cid"))
	password := c.PostForm("password")
	rememberMe := c.PostForm("remember_me") == "on" || c.PostForm("remember_me") == "1" || c.PostForm("remember_me") == "true"

	page := loginPage{
		basePage: basePage{
			CSRFToken: s.issueCSRFToken(c),
		},
		CID:        cidStr,
		RememberMe: rememberMe,
	}

	cid, err := strconv.Atoi(cidStr)
	if err != nil || cid < 1 {
		page.CIDError = "Enter a valid CID"
		s.writeTemplate(c, "login", page)
		return
	}
	if password == "" {
		page.PassError = "Password is required"
		s.writeTemplate(c, "login", page)
		return
	}

	user, err := s.dbRepo.UserRepo.GetUserByCID(context.Background(), cid)
	if err != nil || !s.dbRepo.UserRepo.VerifyPasswordHash(password, user.Password) {
		page.Error = "Bad CID and/or password"
		s.writeTemplate(c, "login", page)
		return
	}

	// Align with FSD policy: suspended/inactive cannot open a web session.
	// Generic error avoids an account-status oracle.
	if user.NetworkRating <= int(protocol.NetworkRatingSuspended) {
		page.Error = "Bad CID and/or password"
		s.writeTemplate(c, "login", page)
		return
	}

	if err := s.setSessionCookie(c, user, rememberMe); err != nil {
		page.Error = "Unable to create session"
		s.writeTemplate(c, "login", page)
		return
	}

	// Rotate CSRF after successful login.
	s.clearCSRFCookie(c)
	s.issueCSRFToken(c)

	c.Redirect(http.StatusSeeOther, "/dashboard")
}

func (s *Server) handleFrontendLogoutPost(c *gin.Context) {
	if !s.validateCSRF(c) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	s.clearSessionCookie(c)
	s.clearCSRFCookie(c)
	c.Redirect(http.StatusSeeOther, "/login")
}
