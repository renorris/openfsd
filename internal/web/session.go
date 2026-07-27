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

const (
	sessionCookieName = "openfsd_session"
	sessionTokenType  = "session"

	sessionDefaultTTL  = 24 * time.Hour
	sessionRememberTTL = 30 * 24 * time.Hour
)

var (
	errNoSessionCookie = errors.New("no session cookie")
	errBadSession      = errors.New("invalid session")
)

// cookieSecureFlag decides whether Set-Cookie should include the Secure attribute.
//
// Policy:
//   - COOKIE_SECURE=true|false|1|0|yes|no → force
//   - unset + TLS listener → Secure
//   - unset + X-Forwarded-Proto: https → Secure
//   - otherwise (local docker-compose HTTP) → not Secure
//
// Operator note: X-Forwarded-Proto is only trustworthy when a reverse proxy
// terminates TLS and overwrites/strips client-supplied XFP. For production
// behind TLS termination, prefer COOKIE_SECURE=true so Secure does not depend
// on client-controlled headers. Spoofing XFP=https on plain HTTP only makes
// browsers drop the cookie (fail-closed for session use on that hop).
func cookieSecureFlag(cookieSecureEnv string, tls bool, xForwardedProto string) bool {
	switch strings.ToLower(strings.TrimSpace(cookieSecureEnv)) {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	}
	if tls {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(xForwardedProto), "https") {
		return true
	}
	return false
}

func (s *Server) cookieSecure(c *gin.Context) bool {
	env := ""
	if s.cfg != nil {
		env = s.cfg.CookieSecure
	}
	tls := c.Request != nil && c.Request.TLS != nil
	proto := ""
	if c.Request != nil {
		proto = c.GetHeader("X-Forwarded-Proto")
	}
	return cookieSecureFlag(env, tls, proto)
}

func (s *Server) jwtSecret() ([]byte, error) {
	secret, err := s.dbRepo.ConfigRepo.Get(db.ConfigJwtSecretKey)
	if err != nil {
		return nil, err
	}
	return []byte(secret), nil
}

// makeSessionCookie signs a stateless session JWT and returns the cookie value + maxAge.
func (s *Server) makeSessionCookie(user *db.User, rememberMe bool) (value string, maxAge int, err error) {
	ttl := sessionDefaultTTL
	if rememberMe {
		ttl = sessionRememberTTL
	}
	maxAge = int(ttl.Seconds())

	token, err := auth.MakeJwtToken(&auth.CustomFields{
		TokenType:     sessionTokenType,
		CID:           user.CID,
		FirstName:     safeStr(user.FirstName),
		LastName:      safeStr(user.LastName),
		NetworkRating: protocol.NetworkRating(user.NetworkRating),
	}, ttl)
	if err != nil {
		return "", 0, err
	}

	secret, err := s.jwtSecret()
	if err != nil {
		return "", 0, err
	}

	value, err = token.SignedString(secret)
	if err != nil {
		return "", 0, err
	}
	return value, maxAge, nil
}

func (s *Server) setSessionCookie(c *gin.Context, user *db.User, rememberMe bool) error {
	value, maxAge, err := s.makeSessionCookie(user, rememberMe)
	if err != nil {
		return err
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cookieSecure(c),
	})
	return nil
}

func (s *Server) clearSessionCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cookieSecure(c),
	})
}

// parseSessionCookie validates the session cookie and returns claims.
func (s *Server) parseSessionCookie(c *gin.Context) (*auth.CustomClaims, error) {
	raw, err := c.Cookie(sessionCookieName)
	if err != nil || raw == "" {
		return nil, errNoSessionCookie
	}

	secret, err := s.jwtSecret()
	if err != nil {
		return nil, err
	}

	token, err := auth.ParseJwtToken(raw, secret)
	if err != nil {
		return nil, errBadSession
	}

	claims := token.CustomClaims()
	if claims.TokenType != sessionTokenType {
		return nil, errBadSession
	}
	if claims.CID < 1 {
		return nil, errBadSession
	}
	return claims, nil
}

// optionalSession loads session claims into the gin context when present (no redirect).
// Revalidates against the DB (KD-9): inactive/missing users get cookies cleared and
// are treated as signed-out so landing does not show stale elevated nav.
func (s *Server) optionalSession(c *gin.Context) *auth.CustomClaims {
	claims, err := s.parseSessionCookie(c)
	if err != nil {
		return nil
	}
	claims, user, err := s.revalidateSessionFromDB(claims)
	if err != nil {
		s.clearSessionCookie(c)
		s.clearCSRFCookie(c)
		return nil
	}
	setJwtContext(c, claims)
	c.Set(dbUserContextKey, user)
	return claims
}
