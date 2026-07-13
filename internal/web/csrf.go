package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"

	"github.com/gin-gonic/gin"
)

const (
	csrfCookieName = "openfsd_csrf"
	csrfFormField  = "csrf_token"
	csrfHeaderName = "X-CSRF-Token"
	csrfTokenBytes = 32
)

// issueCSRFToken creates a random synchronizer token, sets it as a cookie,
// and returns the token for embedding in HTML forms / meta tags.
func (s *Server) issueCSRFToken(c *gin.Context) string {
	// Reuse existing valid cookie when present to keep multi-tab forms working.
	if existing, err := c.Cookie(csrfCookieName); err == nil && len(existing) >= 16 {
		return existing
	}

	buf := make([]byte, csrfTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		// Extremely unlikely; fall back to empty (validate will fail closed).
		return ""
	}
	token := hex.EncodeToString(buf)

	http.SetCookie(c.Writer, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionDefaultTTL.Seconds()),
		HttpOnly: false, // double-submit; JS may read for X-CSRF-Token
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cookieSecure(c),
	})
	return token
}

func (s *Server) clearCSRFCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     csrfCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cookieSecure(c),
	})
}

// csrfTokenFromRequest returns the token from header or form field.
func csrfTokenFromRequest(c *gin.Context) string {
	if h := c.GetHeader(csrfHeaderName); h != "" {
		return h
	}
	if v := c.PostForm(csrfFormField); v != "" {
		return v
	}
	return ""
}

// validateCSRF checks the double-submit synchronizer token.
func (s *Server) validateCSRF(c *gin.Context) bool {
	cookieTok, err := c.Cookie(csrfCookieName)
	if err != nil || cookieTok == "" {
		return false
	}
	reqTok := csrfTokenFromRequest(c)
	if reqTok == "" {
		return false
	}
	if len(cookieTok) != len(reqTok) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cookieTok), []byte(reqTok)) == 1
}

// requireCSRF aborts with 403 when the synchronizer token is missing/invalid.
// Used on HTML form mutation routes.
func (s *Server) requireCSRF(c *gin.Context) {
	if s.validateCSRF(c) {
		c.Next()
		return
	}
	c.AbortWithStatus(http.StatusForbidden)
}

// csrfIfCookieSession enforces CSRF on state-changing requests authenticated
// via the session cookie. CSRF is skipped only when dual-accept auth
// actually succeeded with a valid Bearer access token (auth_method=bearer).
//
// A junk Authorization: Bearer header must NOT disable CSRF while a session
// cookie still authenticates the request (Issue 1 dual-accept bypass).
func (s *Server) csrfIfCookieSession(c *gin.Context) {
	switch c.Request.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		c.Next()
		return
	}

	// Only skip CSRF when jwtBearerMiddleware recorded a successful Bearer auth.
	if method, ok := c.Get(authMethodContextKey); ok && method == authMethodBearer {
		c.Next()
		return
	}

	// Session-authenticated (or any non-bearer) mutation requires CSRF.
	if s.validateCSRF(c) {
		c.Next()
		return
	}

	// JSON 403 for API routes.
	res := newAPIV1Failure("CSRF token missing or invalid")
	writeAPIV1Response(c, http.StatusForbidden, &res)
	c.Abort()
}

func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
