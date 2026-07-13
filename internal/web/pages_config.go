package web

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/internal/auth"
	"github.com/renorris/openfsd/internal/db"
	"github.com/renorris/openfsd/pkg/protocol"
)

func (s *Server) newConfigEditorPage(c *gin.Context) configEditorPage {
	claims := getJwtContext(c)
	page := configEditorPage{
		basePage: basePage{
			User:      pageUserFromClaims(claims),
			CSRFToken: s.issueCSRFToken(c),
		},
		Fields: make([]configField, 0, len(editableConfigKeys)),
	}
	for _, meta := range editableConfigKeys {
		val, err := s.dbRepo.ConfigRepo.Get(meta.Key)
		if err != nil && !errors.Is(err, db.ErrConfigKeyNotFound) {
			// Leave value empty; surface a flash if whole load fails hard.
			val = ""
		}
		if errors.Is(err, db.ErrConfigKeyNotFound) {
			val = ""
		}
		page.Fields = append(page.Fields, configField{
			Key:         meta.Key,
			Label:       meta.Label,
			Description: meta.Description,
			Value:       val,
			Placeholder: meta.Placeholder,
		})
	}
	return page
}

// handleFrontendConfigEditor GET /configeditor — server-rendered config form.
func (s *Server) handleFrontendConfigEditor(c *gin.Context) {
	page := s.newConfigEditorPage(c)
	switch c.Query("flash") {
	case "saved":
		page.FlashSuccess = "Configuration saved"
	case "secret_reset":
		page.FlashSuccess = "JWT secret key reset. All sessions and API tokens are invalidated."
	case "token_created":
		// Token value is not put in the query string (too long / sensitive in logs).
		// The create-token POST re-renders with CreatedToken instead of redirecting.
		page.FlashSuccess = "API token created"
	}
	s.writeTemplate(c, "configeditor", page)
}

// handleFrontendConfigUpdate POST /configeditor — form mutation, no JS required.
func (s *Server) handleFrontendConfigUpdate(c *gin.Context) {
	if !s.validateCSRF(c) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	claims := getJwtContext(c)
	if claims.NetworkRating < protocol.NetworkRatingAdministator {
		c.Redirect(http.StatusSeeOther, "/dashboard")
		return
	}

	page := s.newConfigEditorPage(c)

	// Rebuild fields from posted values (preserve on validation failure).
	for i := range page.Fields {
		key := page.Fields[i].Key
		// Form fields use name="cfg_<KEY>"
		page.Fields[i].Value = c.PostForm("cfg_" + key)
	}

	for i := range page.Fields {
		f := page.Fields[i]
		if !isEditableConfigKey(f.Key) {
			page.FlashError = "Unknown config key"
			s.writeTemplate(c, "configeditor", page)
			return
		}
		if err := s.dbRepo.ConfigRepo.Set(f.Key, f.Value); err != nil {
			page.FlashError = "Error writing configuration"
			s.writeTemplate(c, "configeditor", page)
			return
		}
	}

	c.Redirect(http.StatusSeeOther, "/configeditor?flash=saved")
}

// handleFrontendConfigResetSecret POST /configeditor/reset-secret
func (s *Server) handleFrontendConfigResetSecret(c *gin.Context) {
	if !s.validateCSRF(c) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	claims := getJwtContext(c)
	if claims.NetworkRating < protocol.NetworkRatingAdministator {
		c.Redirect(http.StatusSeeOther, "/dashboard")
		return
	}

	// Require explicit confirmation checkbox for destructive action.
	if c.PostForm("confirm") != "on" && c.PostForm("confirm") != "1" && c.PostForm("confirm") != "yes" {
		page := s.newConfigEditorPage(c)
		page.FlashError = "Confirm the secret reset by checking the confirmation box"
		s.writeTemplate(c, "configeditor", page)
		return
	}

	secretKey, err := db.GenerateJwtSecretKey()
	if err != nil {
		page := s.newConfigEditorPage(c)
		page.FlashError = "Unable to generate secret key"
		s.writeTemplate(c, "configeditor", page)
		return
	}
	if err = s.dbRepo.ConfigRepo.Set(db.ConfigJwtSecretKey, string(secretKey[:])); err != nil {
		page := s.newConfigEditorPage(c)
		page.FlashError = "Unable to store secret key"
		s.writeTemplate(c, "configeditor", page)
		return
	}

	// Current session is now invalid (secret rotated). Clear cookies and send to login.
	s.clearSessionCookie(c)
	s.clearCSRFCookie(c)
	c.Redirect(http.StatusSeeOther, "/login")
}

// handleFrontendConfigCreateToken POST /configeditor/create-token
// Re-renders the page with the token (server-escaped). Copy button is PE only.
func (s *Server) handleFrontendConfigCreateToken(c *gin.Context) {
	if !s.validateCSRF(c) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	claims := getJwtContext(c)
	if claims.NetworkRating < protocol.NetworkRatingAdministator {
		c.Redirect(http.StatusSeeOther, "/dashboard")
		return
	}

	page := s.newConfigEditorPage(c)

	expiryStr := strings.TrimSpace(c.PostForm("expiry_date"))
	var expiry time.Time
	if expiryStr == "" {
		expiry = time.Now().UTC().AddDate(1, 0, 0)
	} else {
		parsed, err := time.Parse("2006-01-02", expiryStr)
		if err != nil {
			page.FlashError = "Invalid expiry date (use YYYY-MM-DD)"
			s.writeTemplate(c, "configeditor", page)
			return
		}
		// End of that UTC day.
		expiry = time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 23, 59, 59, 0, time.UTC)
	}

	now := time.Now()
	if !expiry.After(now) {
		page.FlashError = "Expiry date must be in the future"
		s.writeTemplate(c, "configeditor", page)
		return
	}

	validityDuration := expiry.Sub(now)
	accessToken, err := auth.MakeJwtToken(&auth.CustomFields{
		TokenType:     "access",
		CID:           claims.CID,
		NetworkRating: protocol.NetworkRatingAdministator,
	}, validityDuration)
	if err != nil {
		page.FlashError = "Unable to create token"
		s.writeTemplate(c, "configeditor", page)
		return
	}

	secretKey, err := s.dbRepo.ConfigRepo.Get(db.ConfigJwtSecretKey)
	if err != nil {
		page.FlashError = "Unable to create token"
		s.writeTemplate(c, "configeditor", page)
		return
	}

	tokenStr, err := accessToken.SignedString([]byte(secretKey))
	if err != nil {
		page.FlashError = "Unable to create token"
		s.writeTemplate(c, "configeditor", page)
		return
	}

	page.FlashSuccess = "API token created — copy it now; it will not be shown again"
	page.CreatedToken = tokenStr
	page.TokenExpiry = expiry.UTC().Format(time.RFC3339)
	s.writeTemplate(c, "configeditor", page)
}
