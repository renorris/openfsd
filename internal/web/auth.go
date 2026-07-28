package web

import (
	"context"
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

	user, err := s.dbRepo.UserRepo.GetUserByCID(context.Background(), reqBody.CID)
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

	jwtSecret, err := s.dbRepo.ConfigRepo.Get(context.Background(), db.ConfigJwtSecretKey)
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

	user, err := s.dbRepo.UserRepo.GetUserByCID(context.Background(), claims.CID)
	if err != nil {
		writeAPIV1Response(c, http.StatusUnauthorized, &badTokenRes)
		return
	}
	// Soft-deleted / suspended users must not mint new access tokens.
	if user.NetworkRating <= int(protocol.NetworkRatingSuspended) {
		slog.Debug("refresh rejected inactive/suspended user",
			"cid", claims.CID,
			"event", "refresh_rejected_inactive",
		)
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

	user, err := s.dbRepo.UserRepo.GetUserByCID(context.Background(), cid)
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

	jwtSecret, err := s.dbRepo.ConfigRepo.Get(context.Background(), db.ConfigJwtSecretKey)
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

	jwtSecret, err := s.dbRepo.ConfigRepo.Get(context.Background(), db.ConfigJwtSecretKey)
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

// Session revalidation errors (KD-9).
var (
	errSessionUserMissing = errors.New("session user missing")
	errSessionInactive    = errors.New("session user inactive or suspended")
)

const dbUserContextKey = "db_user"

// revalidateSessionFromDB loads the user by claims.CID, rejects missing or
// inactive/suspended certificates, and overlays NetworkRating + names from the DB.
// Handlers and requireMinRatingHTML keep reading claims.NetworkRating safely only
// because this overlay is mandatory after session cookie parse.
// Also used by revalidateBearerActor for dual-accept Bearer resource requests (KD-18).
func (s *Server) revalidateSessionFromDB(claims *auth.CustomClaims) (*auth.CustomClaims, *db.User, error) {
	user, err := s.dbRepo.UserRepo.GetUserByCID(context.Background(), claims.CID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, errSessionUserMissing
		}
		slog.Error("session revalidation DB error", "cid", claims.CID, "err", err)
		// Fail closed: treat unexpected DB errors like a missing user.
		return nil, nil, errSessionUserMissing
	}
	if user.NetworkRating <= int(protocol.NetworkRatingSuspended) {
		slog.Debug("session rejected inactive/suspended user",
			"cid", claims.CID,
			"event", "session_rejected_inactive",
		)
		return nil, nil, errSessionInactive
	}
	// REQUIRED claims overlay — demotions must refresh ceilings and nav flags.
	claims.NetworkRating = protocol.NetworkRating(user.NetworkRating)
	claims.FirstName = safeStr(user.FirstName)
	claims.LastName = safeStr(user.LastName)
	return claims, user, nil
}

// revalidateBearerActor loads the actor from the DB for Bearer-authenticated
// dual-accept API requests (KD-18). Missing / inactive / suspended → 401.
// Overlays NetworkRating + names so demotions take effect immediately.
// Session cookie path already revalidated in trySessionAuth — skipped here.
func (s *Server) revalidateBearerActor(c *gin.Context) {
	method, _ := c.Get(authMethodContextKey)
	if method != authMethodBearer {
		c.Next()
		return
	}

	claims := getJwtContext(c)
	if claims == nil {
		res := newAPIV1Failure("unauthorized")
		writeAPIV1Response(c, http.StatusUnauthorized, &res)
		c.Abort()
		return
	}

	cid := claims.CID
	claims, user, err := s.revalidateSessionFromDB(claims)
	if err != nil {
		slog.Debug("bearer actor revalidation rejected",
			"cid", cid,
			"event", "bearer_rejected_inactive",
			"err", err.Error(),
		)
		res := newAPIV1Failure("unauthorized")
		writeAPIV1Response(c, http.StatusUnauthorized, &res)
		c.Abort()
		return
	}

	setJwtContext(c, claims)
	c.Set(dbUserContextKey, user)
	c.Next()
}

// trySessionAuth parses the signed session cookie, revalidates against the DB,
// and sets overlaid claims into the gin context (dual-accept API path).
func (s *Server) trySessionAuth(c *gin.Context) bool {
	claims, err := s.parseSessionCookie(c)
	if err != nil {
		return false
	}
	claims, user, err := s.revalidateSessionFromDB(claims)
	if err != nil {
		// Clear cookies so soft-deleted PE clients stop authenticating.
		s.clearSessionCookie(c)
		s.clearCSRFCookie(c)
		return false
	}
	setJwtContext(c, claims)
	c.Set(dbUserContextKey, user)
	return true
}

// requireSessionHTML gates privileged HTML pages: unauthenticated or
// inactive/missing user → clear cookies + 303 /login.
func (s *Server) requireSessionHTML(c *gin.Context) {
	claims, err := s.parseSessionCookie(c)
	if err != nil {
		c.Redirect(http.StatusSeeOther, "/login")
		c.Abort()
		return
	}
	claims, user, err := s.revalidateSessionFromDB(claims)
	if err != nil {
		s.clearSessionCookie(c)
		s.clearCSRFCookie(c)
		c.Redirect(http.StatusSeeOther, "/login")
		c.Abort()
		return
	}
	setJwtContext(c, claims)
	c.Set(dbUserContextKey, user)
	c.Next()
}

// getDBUser returns the *db.User stashed by requireSessionHTML, trySessionAuth,
// or revalidateBearerActor (dual-accept Bearer resource path).
func getDBUser(c *gin.Context) *db.User {
	val, exists := c.Get(dbUserContextKey)
	if !exists {
		return nil
	}
	u, _ := val.(*db.User)
	return u
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
	jwtSecret, err := s.dbRepo.ConfigRepo.Get(context.Background(), db.ConfigJwtSecretKey)
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
