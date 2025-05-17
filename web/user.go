package main

import (
	"database/sql"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/db"
	"github.com/renorris/openfsd/fsd"
	"net/http"
)

// getUserByCID returns the user info of the specified CID.
//
// Only >= SUP can request CIDs other than what is indicated in their bearer token.
func (s *Server) getUserByCID(c *gin.Context) {
	type RequestBody struct {
		CID int `json:"cid" binding:"min=1,required"`
	}

	var reqBody RequestBody
	if !bindJSONOrAbort(c, &reqBody) {
		return
	}

	claims := getJwtContext(c)

	if reqBody.CID != claims.CID && claims.NetworkRating < fsd.NetworkRatingSupervisor {
		writeAPIV1Response(c, http.StatusForbidden, &genericAPIV1Forbidden)
		return
	}

	user, err := s.dbRepo.UserRepo.GetUserByCID(reqBody.CID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeAPIV1Response(c, http.StatusNotFound, &genericAPIV1NotFound)
			return
		}
		writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
		return
	}

	type ResponseBody struct {
		CID           int    `json:"cid"`
		FirstName     string `json:"first_name"`
		LastName      string `json:"last_name"`
		NetworkRating int    `json:"network_rating"`
	}

	resBody := ResponseBody{
		CID:           user.CID,
		FirstName:     safeStr(user.FirstName),
		LastName:      safeStr(user.LastName),
		NetworkRating: user.NetworkRating,
	}

	res := newAPIV1Success(&resBody)
	writeAPIV1Response(c, http.StatusOK, &res)
}

// updateUser updates the user with a specified CID.
//
// The CID itself is immutable and cannot be changed.
// Only >= SUP can update CIDs other than what is indicated in their bearer token.
func (s *Server) updateUser(c *gin.Context) {
	claims := getJwtContext(c)
	if claims.NetworkRating < fsd.NetworkRatingSupervisor {
		writeAPIV1Response(c, http.StatusForbidden, &genericAPIV1Forbidden)
		return
	}

	type RequestBody struct {
		CID           int     `json:"cid" binding:"min=1,required"`
		Password      *string `json:"password"`
		FirstName     *string `json:"first_name"`
		LastName      *string `json:"last_name"`
		NetworkRating *int    `json:"network_rating" binding:"min=-1,max=12"`
	}

	var reqBody RequestBody
	if !bindJSONOrAbort(c, &reqBody) {
		return
	}

	targetUser, err := s.dbRepo.UserRepo.GetUserByCID(reqBody.CID)
	if err != nil {
		writeAPIV1Response(c, http.StatusNotFound, &genericAPIV1NotFound)
		return
	}

	if targetUser.NetworkRating > int(claims.NetworkRating) {
		res := newAPIV1Failure("cannot update user with higher network rating")
		writeAPIV1Response(c, http.StatusForbidden, &res)
		return
	}

	// Update target user's fields depending on what was provided in the request
	if reqBody.Password != nil {
		targetUser.Password = *reqBody.Password
	}
	if reqBody.FirstName != nil {
		targetUser.FirstName = reqBody.FirstName
	}
	if reqBody.LastName != nil {
		targetUser.LastName = reqBody.LastName
	}
	if reqBody.NetworkRating != nil {
		targetUser.NetworkRating = *reqBody.NetworkRating
	}

	err = s.dbRepo.UserRepo.UpdateUser(targetUser)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeAPIV1Response(c, http.StatusNotFound, &genericAPIV1NotFound)
			return
		}
		writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
		return
	}

	type ResponseBody struct {
		CID           int    `json:"cid"`
		FirstName     string `json:"first_name"`
		LastName      string `json:"last_name"`
		NetworkRating int    `json:"network_rating"`
	}

	resBody := ResponseBody{
		CID:           targetUser.CID,
		FirstName:     safeStr(targetUser.FirstName),
		LastName:      safeStr(targetUser.LastName),
		NetworkRating: targetUser.NetworkRating,
	}

	res := newAPIV1Success(&resBody)
	writeAPIV1Response(c, http.StatusOK, &res)
}

func (s *Server) createUser(c *gin.Context) {
	type RequestBody struct {
		Password      string  `json:"password" binding:"min=8,required"`
		FirstName     *string `json:"first_name"`
		LastName      *string `json:"last_name"`
		NetworkRating int     `json:"network_rating" binding:"min=-1,max=12,required"`
	}

	var reqBody RequestBody
	if !bindJSONOrAbort(c, &reqBody) {
		return
	}

	claims := getJwtContext(c)
	if claims.NetworkRating < fsd.NetworkRatingSupervisor ||
		reqBody.NetworkRating > int(claims.NetworkRating) {
		writeAPIV1Response(c, http.StatusForbidden, &genericAPIV1Forbidden)
		return
	}

	user := &db.User{
		Password:      reqBody.Password,
		FirstName:     reqBody.FirstName,
		LastName:      reqBody.LastName,
		NetworkRating: reqBody.NetworkRating,
	}

	if err := s.dbRepo.UserRepo.CreateUser(user); err != nil {
		writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
		return
	}

	type ResponseBody struct {
		CID           int     `json:"cid"`
		FirstName     *string `json:"first_name"`
		LastName      *string `json:"last_name"`
		NetworkRating int     `json:"network_rating" binding:"min=-1,max=12,required"`
	}

	resBody := ResponseBody{
		CID:           user.CID,
		FirstName:     user.FirstName,
		LastName:      user.LastName,
		NetworkRating: user.NetworkRating,
	}

	res := newAPIV1Success(&resBody)
	writeAPIV1Response(c, http.StatusCreated, &res)
}
