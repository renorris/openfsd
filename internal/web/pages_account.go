package web

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/pkg/protocol"
)

// handleFrontendAccount GET /account — profile + change-password + delete forms.
func (s *Server) handleFrontendAccount(c *gin.Context) {
	claims, ok := requireJwtContext(c)
	if !ok {
		return
	}
	page := s.loadAccountPage(c, claims.CID)
	switch c.Query("flash") {
	case "password_changed":
		page.FlashSuccess = "Password changed successfully."
	}
	s.writeTemplate(c, "account", page)
}

func (s *Server) loadAccountPage(c *gin.Context, cid int) accountPage {
	claims, _ := requireJwtContext(c)
	page := accountPage{
		basePage: basePage{
			User:      pageUserFromClaims(claims),
			CSRFToken: s.issueCSRFToken(c),
		},
		CID:                  cid,
		AllowPermanentDelete: s.cfg != nil && s.cfg.AllowPermanentAccountDelete,
	}

	user := getDBUser(c)
	if user == nil || user.CID != cid {
		var err error
		user, err = s.dbRepo.UserRepo.GetUserByCID(cid)
		if err != nil {
			page.FormError = "Unable to load account"
			return page
		}
	}
	page.FirstName = safeStr(user.FirstName)
	page.LastName = safeStr(user.LastName)
	page.NetworkLabel = networkRatingLabel(user.NetworkRating)
	page.PilotLabel = pilotRatingLabel(user.PilotRating)
	return page
}

// handleFrontendAccountPassword POST /account/password
func (s *Server) handleFrontendAccountPassword(c *gin.Context) {
	if !s.validateCSRF(c) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	claims, ok := requireJwtContext(c)
	if !ok {
		return
	}

	current := c.PostForm("current_password")
	newPW := c.PostForm("new_password")
	confirm := c.PostForm("confirm_password")

	page := s.loadAccountPage(c, claims.CID)

	user, err := s.dbRepo.UserRepo.GetUserByCID(claims.CID)
	if err != nil {
		s.clearSessionCookie(c)
		s.clearCSRFCookie(c)
		c.Redirect(http.StatusSeeOther, "/login")
		return
	}

	if current == "" {
		page.CurrentPassError = "Current password is required"
		s.writeTemplate(c, "account", page)
		return
	}
	if !s.dbRepo.UserRepo.VerifyPasswordHash(current, user.Password) {
		slog.Debug("password change rejected bad current", "cid", claims.CID)
		page.CurrentPassError = "Incorrect password"
		s.writeTemplate(c, "account", page)
		return
	}
	if msg := validateNewPassword(newPW); msg != "" {
		page.NewPassError = msg
		s.writeTemplate(c, "account", page)
		return
	}
	if newPW != confirm {
		page.ConfirmPassError = "Passwords do not match"
		s.writeTemplate(c, "account", page)
		return
	}
	if newPW == current {
		page.NewPassError = "New password must be different from the current password"
		s.writeTemplate(c, "account", page)
		return
	}

	user.Password = newPW
	if err := s.dbRepo.UserRepo.UpdateUser(user); err != nil {
		slog.Error("account password update failed", "cid", claims.CID, "err", err)
		page.FormError = "Unable to update password"
		s.writeTemplate(c, "account", page)
		return
	}

	// KD-7: re-issue session with rememberMe=false (24h); rotate CSRF.
	if err := s.setSessionCookie(c, user, false); err != nil {
		slog.Error("account password session reissue failed", "cid", claims.CID, "err", err)
		page.FormError = "Password updated but session could not be refreshed; please log in again"
		s.writeTemplate(c, "account", page)
		return
	}
	s.clearCSRFCookie(c)
	s.issueCSRFToken(c)

	slog.Info("account password changed",
		"cid", claims.CID,
		"event", "account_password_changed",
	)
	c.Redirect(http.StatusSeeOther, "/account?flash=password_changed")
}

// handleFrontendAccountDelete POST /account/delete
func (s *Server) handleFrontendAccountDelete(c *gin.Context) {
	if !s.validateCSRF(c) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	claims, ok := requireJwtContext(c)
	if !ok {
		return
	}

	current := c.PostForm("current_password")
	confirmCID := strings.TrimSpace(c.PostForm("confirm_cid"))
	wantPermanent := c.PostForm("permanent") == "1" || c.PostForm("permanent") == "on"

	page := s.loadAccountPage(c, claims.CID)

	user, err := s.dbRepo.UserRepo.GetUserByCID(claims.CID)
	if err != nil || user.NetworkRating <= int(protocol.NetworkRatingSuspended) {
		s.clearSessionCookie(c)
		s.clearCSRFCookie(c)
		c.Redirect(http.StatusSeeOther, "/login")
		return
	}

	if current == "" {
		page.DeletePassError = "Current password is required"
		s.writeTemplate(c, "account", page)
		return
	}
	if !s.dbRepo.UserRepo.VerifyPasswordHash(current, user.Password) {
		slog.Debug("account delete rejected bad password", "cid", claims.CID)
		page.DeletePassError = "Incorrect password"
		s.writeTemplate(c, "account", page)
		return
	}
	if confirmCID != strconv.Itoa(user.CID) {
		page.DeleteError = "Confirm your CID exactly to delete the account"
		s.writeTemplate(c, "account", page)
		return
	}

	allowHard := s.cfg != nil && s.cfg.AllowPermanentAccountDelete
	permanentDisabled := wantPermanent && !allowHard

	if wantPermanent && allowHard {
		if err := s.dbRepo.UserRepo.DeleteUser(user.CID); err != nil {
			slog.Error("account hard-delete failed", "cid", claims.CID, "err", err)
			page.DeleteError = "Unable to delete account"
			s.writeTemplate(c, "account", page)
			return
		}
		slog.Info("account deleted",
			"cid", claims.CID,
			"event", "account_deleted",
			"mode", "hard",
		)
	} else {
		// Soft-delete (default, or permanent requested but disabled).
		user.NetworkRating = int(protocol.NetworkRatingInactive)
		user.Password = "" // keep existing hash
		if err := s.dbRepo.UserRepo.UpdateUser(user); err != nil {
			slog.Error("account soft-delete failed", "cid", claims.CID, "err", err)
			page.DeleteError = "Unable to delete account"
			s.writeTemplate(c, "account", page)
			return
		}
		slog.Info("account deleted",
			"cid", claims.CID,
			"event", "account_deleted",
			"mode", "soft",
		)
		_ = permanentDisabled
	}

	s.clearSessionCookie(c)
	s.clearCSRFCookie(c)

	loc := "/login?account=deleted"
	if permanentDisabled {
		loc = "/login?account=deleted&permanent=disabled"
	}
	c.Redirect(http.StatusSeeOther, loc)
}
