package web

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/internal/auth"
	"github.com/renorris/openfsd/internal/db"
	"github.com/renorris/openfsd/pkg/protocol"
)

// getAccessRefreshTokens returns access and refresh tokens given FSD login credentials
func (s *Server) getAccessRefreshTokens(c *gin.Context) {
	type RequestBody struct {
		CID        int    `json:"cid" binding:"min=1,required"`
		Password   string `json:"password" binding:"required"`
		RememberMe bool   `json:"remember_me"`
	}

	var reqBody RequestBody
	if !bindJSONOrAbort(c, &reqBody) {
		return
	}

	unauthRes := newAPIV1Failure("Bad CID and/or password")

	user, err := s.dbRepo.UserRepo.GetUserByCID(reqBody.CID)
	if err != nil {
		writeAPIV1Response(c, http.StatusUnauthorized, &unauthRes)
		return
	}

	if !s.dbRepo.UserRepo.VerifyPasswordHash(reqBody.Password, user.Password) {
		writeAPIV1Response(c, http.StatusUnauthorized, &unauthRes)
		return
	}

	// Align with FSD policy: suspended/inactive cannot mint web tokens.
	if user.NetworkRating <= int(protocol.NetworkRatingSuspended) {
		writeAPIV1Response(c, http.StatusUnauthorized, &unauthRes)
		return
	}

	access, refresh, err := s.makeAccessRefreshTokens(user, reqBody.RememberMe)
	if err != nil {
		writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
		return
	}

	type ResponseBody struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}

	resBody := ResponseBody{
		AccessToken:  access,
		RefreshToken: refresh,
	}

	res := newAPIV1Success(&resBody)
	c.JSON(http.StatusOK, &res)
}

// refreshAccessToken refreshes an access token given a refresh token
func (s *Server) refreshAccessToken(c *gin.Context) {
	type RequestBody struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}

	var reqBody RequestBody
	if !bindJSONOrAbort(c, &reqBody) {
		return
	}

	badTokenRes := newAPIV1Failure("bad token")

	jwtSecret, err := s.dbRepo.ConfigRepo.Get(db.ConfigJwtSecretKey)
	if err != nil {
		writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
		return
	}

	refreshToken, err := auth.ParseJwtToken(reqBody.RefreshToken, []byte(jwtSecret))
	if err != nil {
		writeAPIV1Response(c, http.StatusUnauthorized, &badTokenRes)
		return
	}

	claims := refreshToken.CustomClaims()

	if claims.TokenType != "refresh" {
		writeAPIV1Response(c, http.StatusUnauthorized, &badTokenRes)
		return
	}

	user, err := s.dbRepo.UserRepo.GetUserByCID(claims.CID)
	if err != nil {
		writeAPIV1Response(c, http.StatusUnauthorized, &badTokenRes)
		return
	}

	access, err := s.makeAccessToken(user, []byte(jwtSecret))
	if err != nil {
		writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
		return
	}

	type ResponseBody struct {
		AccessToken string `json:"access_token"`
	}

	resBody := ResponseBody{
		AccessToken: access,
	}

	res := newAPIV1Success(&resBody)
	c.JSON(http.StatusOK, &res)
}

func (s *Server) getFsdJwt(c *gin.Context) {
	type RequestBody struct {
		CID      string `json:"cid" form:"cid" binding:"required"`
		Password string `json:"password" form:"password" binding:"required"`
	}

	type ResponseBody struct {
		Success  bool   `json:"success"`
		Token    string `json:"token,omitempty"`
		ErrorMsg string `json:"error_msg,omitempty"`
	}

	var reqBody RequestBody
	if err := c.ShouldBind(&reqBody); err != nil {
		c.JSON(http.StatusBadRequest, &ResponseBody{ErrorMsg: "Invalid request"})
		return
	}

	cid, err := strconv.Atoi(reqBody.CID)
	if err != nil || cid < 1 {
		c.JSON(http.StatusBadRequest, &ResponseBody{ErrorMsg: "Invalid CID"})
		return
	}

	user, err := s.dbRepo.UserRepo.GetUserByCID(cid)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusUnauthorized, &ResponseBody{ErrorMsg: "Invalid CID and/or password"})
			return
		}

		c.JSON(http.StatusInternalServerError, &ResponseBody{ErrorMsg: "Internal server error"})
		return
	}

	if !s.dbRepo.UserRepo.VerifyPasswordHash(reqBody.Password, user.Password) {
		c.JSON(http.StatusUnauthorized, &ResponseBody{ErrorMsg: "Invalid CID and/or password"})
		return
	}

	if !s.dbRepo.UserRepo.VerifyPasswordHash(reqBody.Password, user.Password) {
		resBody := ResponseBody{
			ErrorMsg: "Invalid CID and/or password",
		}
		c.JSON(http.StatusUnauthorized, &resBody)
		return
	}

	if user.NetworkRating <= int(protocol.NetworkRatingSuspended) {
		c.JSON(http.StatusForbidden, &ResponseBody{ErrorMsg: "Certificate suspended or inactive"})
		return
	}

	fsdJwtToken, err := auth.MakeJwtToken(&auth.CustomFields{
		TokenType:     "fsd",
		CID:           user.CID,
		FirstName:     safeStr(user.FirstName),
		LastName:      safeStr(user.LastName),
		NetworkRating: protocol.NetworkRating(user.NetworkRating),
	}, 5*time.Minute)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &ResponseBody{ErrorMsg: "Internal server error"})
		return
	}

	jwtSecret, err := s.dbRepo.ConfigRepo.Get(db.ConfigJwtSecretKey)
	if err != nil {
		writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
		return
	}

	fsdJwtTokenStr, err := fsdJwtToken.SignedString([]byte(jwtSecret))
	if err != nil {
		c.JSON(http.StatusInternalServerError, &ResponseBody{ErrorMsg: "Internal server error"})
		return
	}

	c.JSON(http.StatusOK, &ResponseBody{
		Success: true,
		Token:   fsdJwtTokenStr,
	})
}

// Auth method keys for dual-accept (Bearer vs session cookie).
// csrfIfCookieSession only skips CSRF when auth actually succeeded via Bearer.
const (
	authMethodContextKey = "auth_method"
	authMethodBearer     = "bearer"
	authMethodSession    = "session"
)

// jwtBearerMiddleware verifies a Bearer access token OR a signed session cookie
// (KD-18 dual-accept). Cookie-authenticated mutations are CSRF-checked by
// csrfIfCookieSession on the API group.
func (s *Server) jwtBearerMiddleware(c *gin.Context) {
	if s.tryBearerAuth(c) {
		c.Set(authMethodContextKey, authMethodBearer)
		c.Next()
		return
	}
	if s.trySessionAuth(c) {
		c.Set(authMethodContextKey, authMethodSession)
		c.Next()
		return
	}

	res := newAPIV1Failure("unauthorized")
	writeAPIV1Response(c, http.StatusUnauthorized, &res)
	c.Abort()
}

// tryBearerAuth parses Authorization: Bearer access tokens into the gin context.
// Returns true when a valid access token was accepted.
// The scheme match is case-insensitive ("Bearer " / "bearer ").
func (s *Server) tryBearerAuth(c *gin.Context) bool {
	raw, ok := cutBearerToken(c.GetHeader("Authorization"))
	if !ok {
		return false
	}

	jwtSecret, err := s.dbRepo.ConfigRepo.Get(db.ConfigJwtSecretKey)
	if err != nil {
		return false
	}

	accessToken, err := auth.ParseJwtToken(raw, []byte(jwtSecret))
	if err != nil {
		return false
	}

	claims := accessToken.CustomClaims()
	if claims.TokenType != "access" {
		return false
	}

	setJwtContext(c, claims)
	return true
}

// cutBearerToken extracts the token from an Authorization header with a
// case-insensitive "Bearer " scheme. Empty tokens are rejected.
func cutBearerToken(header string) (token string, ok bool) {
	if len(header) < 7 {
		return "", false
	}
	if !equalFoldASCII(header[:7], "Bearer ") {
		return "", false
	}
	token = strings.TrimSpace(header[7:])
	if token == "" {
		return "", false
	}
	return token, true
}

// trySessionAuth parses the signed session cookie into the gin context.
func (s *Server) trySessionAuth(c *gin.Context) bool {
	claims, err := s.parseSessionCookie(c)
	if err != nil {
		return false
	}
	setJwtContext(c, claims)
	return true
}

// requireSessionHTML gates privileged HTML pages: unauthenticated → 303 /login.
func (s *Server) requireSessionHTML(c *gin.Context) {
	claims, err := s.parseSessionCookie(c)
	if err != nil {
		c.Redirect(http.StatusSeeOther, "/login")
		c.Abort()
		return
	}
	setJwtContext(c, claims)
	c.Next()
}

// requireMinRatingHTML redirects to /dashboard when the session rating is too low.
func (s *Server) requireMinRatingHTML(min protocol.NetworkRating) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := requireJwtContext(c)
		if !ok {
			return
		}
		if claims.NetworkRating < min {
			c.Redirect(http.StatusSeeOther, "/dashboard")
			c.Abort()
			return
		}
		c.Next()
	}
}

const jwtContextKey = "jwtbearer"

func setJwtContext(c *gin.Context, claims *auth.CustomClaims) {
	c.Set(jwtContextKey, claims)
}

// getJwtContext returns session/bearer claims set by requireSessionHTML /
// jwtBearerMiddleware. Returns nil if missing or wrong type (never panics).
func getJwtContext(c *gin.Context) *auth.CustomClaims {
	val, exists := c.Get(jwtContextKey)
	if !exists {
		return nil
	}
	claims, ok := val.(*auth.CustomClaims)
	if !ok || claims == nil {
		return nil
	}
	return claims
}

// requireJwtContext is the non-optional handler path: aborts with 500 and
// returns false when claims are missing. HTML and JSON handlers both use it.
func requireJwtContext(c *gin.Context) (*auth.CustomClaims, bool) {
	claims := getJwtContext(c)
	if claims == nil {
		slog.Error("jwt context missing on authenticated handler path",
			"path", c.Request.URL.Path,
			"method", c.Request.Method,
		)
		c.AbortWithStatus(http.StatusInternalServerError)
		return nil, false
	}
	return claims, true
}

func (s *Server) makeAccessRefreshTokens(user *db.User, rememberMe bool) (access string, refresh string, err error) {
	jwtSecret, err := s.dbRepo.ConfigRepo.Get(db.ConfigJwtSecretKey)
	if err != nil {
		return
	}

	access, err = s.makeAccessToken(user, []byte(jwtSecret))
	if err != nil {
		return
	}

	refresh, err = s.makeRefreshToken(user, rememberMe, []byte(jwtSecret))
	if err != nil {
		return
	}

	return
}

func (s *Server) makeAccessToken(user *db.User, jwtSecret []byte) (access string, err error) {
	// Make access token
	accessToken, err := auth.MakeJwtToken(&auth.CustomFields{
		TokenType:     "access",
		CID:           user.CID,
		FirstName:     safeStr(user.FirstName),
		LastName:      safeStr(user.LastName),
		NetworkRating: protocol.NetworkRating(user.NetworkRating),
	}, 15*time.Minute)
	if err != nil {
		return
	}

	access, err = accessToken.SignedString(jwtSecret)
	if err != nil {
		return
	}

	return
}

func (s *Server) makeRefreshToken(user *db.User, rememberMe bool, jwtSecret []byte) (refresh string, err error) {
	refreshTokenDuration := time.Hour * 24
	if rememberMe {
		refreshTokenDuration = time.Hour * 24 * 30
	}

	// Make refresh token
	refreshToken, err := auth.MakeJwtToken(&auth.CustomFields{
		TokenType:     "refresh",
		CID:           user.CID,
		FirstName:     safeStr(user.FirstName),
		LastName:      safeStr(user.LastName),
		NetworkRating: protocol.NetworkRating(user.NetworkRating),
	}, refreshTokenDuration)
	if err != nil {
		return
	}

	refresh, err = refreshToken.SignedString(jwtSecret)
	if err != nil {
		return
	}

	return
}
