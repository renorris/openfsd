package main

import (
	"database/sql"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/db"
	"github.com/renorris/openfsd/fsd"
	"net/http"
	"strconv"
	"strings"
	"time"
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

	refreshToken, err := fsd.ParseJwtToken(reqBody.RefreshToken, []byte(jwtSecret))
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

	var reqBody RequestBody
	if err := c.ShouldBind(&reqBody); err != nil {
		return
	}

	type ResponseBody struct {
		Success  bool   `json:"success"`
		Token    string `json:"token,omitempty"`
		ErrorMsg string `json:"error_msg,omitempty"`
	}

	cid, err := strconv.Atoi(reqBody.CID)
	if err != nil || cid < 1 {
		resBody := ResponseBody{
			ErrorMsg: "Invalid CID",
		}
		c.JSON(http.StatusBadRequest, &resBody)
		return
	}

	user, err := s.dbRepo.UserRepo.GetUserByCID(cid)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			resBody := ResponseBody{
				ErrorMsg: "Invalid CID and/or password",
			}
			c.JSON(http.StatusUnauthorized, &resBody)
			return
		}

		resBody := ResponseBody{
			ErrorMsg: "Internal server error",
		}
		c.JSON(http.StatusInternalServerError, &resBody)
		return
	}

	if user.NetworkRating <= int(fsd.NetworkRatingSuspended) {
		resBody := ResponseBody{
			ErrorMsg: "Certificate suspended or inactive",
		}
		c.JSON(http.StatusForbidden, &resBody)
		return
	}

	fsdJwtToken, err := fsd.MakeJwtToken(&fsd.CustomFields{
		TokenType:     "fsd",
		CID:           user.CID,
		FirstName:     safeStr(user.FirstName),
		LastName:      safeStr(user.LastName),
		NetworkRating: fsd.NetworkRating(user.NetworkRating),
	}, 5*time.Minute)
	if err != nil {
		resBody := ResponseBody{
			ErrorMsg: "Internal server error",
		}
		c.JSON(http.StatusInternalServerError, &resBody)
		return
	}

	jwtSecret, err := s.dbRepo.ConfigRepo.Get(db.ConfigJwtSecretKey)
	if err != nil {
		writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
		return
	}

	fsdJwtTokenStr, err := fsdJwtToken.SignedString([]byte(jwtSecret))
	if err != nil {
		resBody := ResponseBody{
			ErrorMsg: "Internal server error",
		}
		c.JSON(http.StatusInternalServerError, &resBody)
		return
	}

	resBody := ResponseBody{
		Success: true,
		Token:   fsdJwtTokenStr,
	}

	c.JSON(http.StatusOK, &resBody)
}

// jwtBearerMiddleware verifies the existence of, validates, and parses JWT bearer tokens.
//
// No specific validation of verified claims are done in this function.
func (s *Server) jwtBearerMiddleware(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")
	authHeader, found := strings.CutPrefix(authHeader, "Bearer ")
	if !found {
		res := newAPIV1Failure("bad bearer token")
		writeAPIV1Response(c, http.StatusBadRequest, &res)
		c.Abort()
		return
	}

	jwtSecret, err := s.dbRepo.ConfigRepo.Get(db.ConfigJwtSecretKey)
	if err != nil {
		writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
		c.Abort()
		return
	}

	accessToken, err := fsd.ParseJwtToken(authHeader, []byte(jwtSecret))
	if err != nil {
		res := newAPIV1Failure("invalid bearer token")
		writeAPIV1Response(c, http.StatusUnauthorized, &res)
		c.Abort()
		return
	}

	claims := accessToken.CustomClaims()

	if claims.TokenType != "access" {
		res := newAPIV1Failure("invalid token type")
		writeAPIV1Response(c, http.StatusUnauthorized, &res)
		c.Abort()
		return
	}

	setJwtContext(c, claims)

	c.Next()
}

const jwtContextKey = "jwtbearer"

func setJwtContext(c *gin.Context, claims *fsd.CustomClaims) {
	c.Set(jwtContextKey, claims)
}

func getJwtContext(c *gin.Context) (claims *fsd.CustomClaims) {
	val, exists := c.Get(jwtContextKey)
	if !exists {
		panic("attempted to load non-existent jwt context")
	}

	claims = val.(*fsd.CustomClaims)

	return
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
	accessToken, err := fsd.MakeJwtToken(&fsd.CustomFields{
		TokenType:     "access",
		CID:           user.CID,
		FirstName:     safeStr(user.FirstName),
		LastName:      safeStr(user.LastName),
		NetworkRating: fsd.NetworkRating(user.NetworkRating),
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
	refreshToken, err := fsd.MakeJwtToken(&fsd.CustomFields{
		TokenType:     "refresh",
		CID:           user.CID,
		FirstName:     safeStr(user.FirstName),
		LastName:      safeStr(user.LastName),
		NetworkRating: fsd.NetworkRating(user.NetworkRating),
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
