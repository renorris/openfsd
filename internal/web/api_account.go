package web

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/pkg/protocol"
)

// setupAccountAPIRoutes mounts Provisional account self-service JSON under
// /api/v1/account (password change + soft/hard delete). Dual-accept OBS+ self.
// Stability: Provisional until post-release maintainer sign-off
// (docs/design/rest-api-versioning.md §C).
func (s *Server) setupAccountAPIRoutes(parent *gin.RouterGroup) {
	g := parent.Group("/account")
	s.useAPIV1Protected(g)
	g.POST("/password", s.handleAPIAccountPassword)
	g.POST("/delete", s.handleAPIAccountDelete)
}

// handleAPIAccountPassword POST /api/v1/account/password
//
// Request: { current_password, new_password, confirm_password }
// Validation parity with pages_account.go / validateNewPassword.
// Bearer: password update only (no session cookie side effects).
// Cookie dual-accept: re-issue 24h session + clear CSRF like HTML.
func (s *Server) handleAPIAccountPassword(c *gin.Context) {
	claims, ok := requireJwtContext(c)
	if !ok {
		return
	}

	var reqBody struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
		ConfirmPassword string `json:"confirm_password"`
	}
	if !bindJSONOrAbort(c, &reqBody) {
		return
	}

	user, err := s.dbRepo.UserRepo.GetUserByCID(context.Background(), claims.CID)
	if err != nil {
		// Actor was valid at middleware; treat as unauthorized if gone mid-request.
		res := newAPIV1Failure("unauthorized")
		writeAPIV1Response(c, http.StatusUnauthorized, &res)
		return
	}

	if reqBody.CurrentPassword == "" {
		res := newAPIV1Failure("current password is required")
		writeAPIV1Response(c, http.StatusBadRequest, &res)
		return
	}
	if !s.dbRepo.UserRepo.VerifyPasswordHash(reqBody.CurrentPassword, user.Password) {
		slog.Debug("api password change rejected bad current", "cid", claims.CID)
		res := newAPIV1Failure("incorrect password")
		writeAPIV1Response(c, http.StatusBadRequest, &res)
		return
	}
	if msg := validateNewPassword(reqBody.NewPassword); msg != "" {
		res := newAPIV1Failure(msg)
		writeAPIV1Response(c, http.StatusBadRequest, &res)
		return
	}
	if reqBody.ConfirmPassword == "" || reqBody.NewPassword != reqBody.ConfirmPassword {
		res := newAPIV1Failure("Passwords do not match")
		writeAPIV1Response(c, http.StatusBadRequest, &res)
		return
	}
	if reqBody.NewPassword == reqBody.CurrentPassword {
		res := newAPIV1Failure("New password must be different from the current password")
		writeAPIV1Response(c, http.StatusBadRequest, &res)
		return
	}

	user.Password = reqBody.NewPassword
	if err := s.dbRepo.UserRepo.UpdateUser(context.Background(), user); err != nil {
		slog.Error("api account password update failed", "cid", claims.CID, "err", err)
		writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
		return
	}

	// Cookie dual-accept: parity with HTML (KD-7 session re-issue). Bearer: none.
	if isSessionAuth(c) {
		if err := s.setSessionCookie(c, user, false); err != nil {
			slog.Error("api account password session reissue failed", "cid", claims.CID, "err", err)
			// Password already updated; report success (mutation cannot roll back cleanly).
		} else {
			s.clearCSRFCookie(c)
		}
	}

	slog.Info("account password changed",
		"cid", claims.CID,
		"event", "account_password_changed",
		"auth_method", authMethodOf(c),
	)
	res := newAPIV1Success(nil)
	writeAPIV1Response(c, http.StatusOK, &res)
}

// handleAPIAccountDelete POST /api/v1/account/delete
//
// Request: { current_password, confirm_cid, permanent? }
// Soft-delete default. permanent:true is fail-closed when hard-delete is
// disabled (intentional JSON divergence from HTML — no silent soft-delete).
// Bearer: no cookie clear. Cookie dual-accept: clear session + CSRF like HTML.
func (s *Server) handleAPIAccountDelete(c *gin.Context) {
	claims, ok := requireJwtContext(c)
	if !ok {
		return
	}

	var reqBody struct {
		CurrentPassword string `json:"current_password"`
		ConfirmCID      int    `json:"confirm_cid"`
		Permanent       bool   `json:"permanent"`
	}
	if !bindJSONOrAbort(c, &reqBody) {
		return
	}

	user, err := s.dbRepo.UserRepo.GetUserByCID(context.Background(), claims.CID)
	if err != nil {
		res := newAPIV1Failure("unauthorized")
		writeAPIV1Response(c, http.StatusUnauthorized, &res)
		return
	}

	if reqBody.CurrentPassword == "" {
		res := newAPIV1Failure("current password is required")
		writeAPIV1Response(c, http.StatusBadRequest, &res)
		return
	}
	if !s.dbRepo.UserRepo.VerifyPasswordHash(reqBody.CurrentPassword, user.Password) {
		slog.Debug("api account delete rejected bad password", "cid", claims.CID)
		res := newAPIV1Failure("incorrect password")
		writeAPIV1Response(c, http.StatusBadRequest, &res)
		return
	}
	if reqBody.ConfirmCID != user.CID {
		res := newAPIV1Failure("confirm_cid must match your CID")
		writeAPIV1Response(c, http.StatusBadRequest, &res)
		return
	}

	allowHard := s.cfg != nil && s.cfg.AllowPermanentAccountDelete
	if reqBody.Permanent && !allowHard {
		// Fail-closed: automation must not silently get soft-delete when it
		// requested permanent. Retry with permanent:false for soft-delete.
		res := newAPIV1Failure("permanent delete is disabled")
		writeAPIV1Response(c, http.StatusBadRequest, &res)
		return
	}

	status := "soft_deleted"
	if reqBody.Permanent && allowHard {
		if err := s.dbRepo.UserRepo.DeleteUser(context.Background(), user.CID); err != nil {
			slog.Error("api account hard-delete failed", "cid", claims.CID, "err", err)
			writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
			return
		}
		status = "hard_deleted"
		slog.Info("account deleted",
			"cid", claims.CID,
			"event", "account_deleted",
			"mode", "hard",
			"auth_method", authMethodOf(c),
		)
	} else {
		// Soft-delete: Inactive rating; empty Password keeps existing hash (UpdateUser).
		user.NetworkRating = int(protocol.NetworkRatingInactive)
		user.Password = ""
		if err := s.dbRepo.UserRepo.UpdateUser(context.Background(), user); err != nil {
			slog.Error("api account soft-delete failed", "cid", claims.CID, "err", err)
			writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
			return
		}
		slog.Info("account deleted",
			"cid", claims.CID,
			"event", "account_deleted",
			"mode", "soft",
			"auth_method", authMethodOf(c),
		)
	}

	if isSessionAuth(c) {
		s.clearSessionCookie(c)
		s.clearCSRFCookie(c)
	}

	res := newAPIV1Success(map[string]string{"status": status})
	writeAPIV1Response(c, http.StatusOK, &res)
}

func isSessionAuth(c *gin.Context) bool {
	m, _ := c.Get(authMethodContextKey)
	return m == authMethodSession
}

func authMethodOf(c *gin.Context) string {
	m, _ := c.Get(authMethodContextKey)
	if s, ok := m.(string); ok && s != "" {
		return s
	}
	return "unknown"
}
