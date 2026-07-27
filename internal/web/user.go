package web

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/internal/db"
	"github.com/renorris/openfsd/pkg/protocol"
)

// getUserByCID returns the user info of the specified CID.
//
// Self always allowed. Other CIDs require Instructor1+ (directory / rating tooling).
func (s *Server) getUserByCID(c *gin.Context) {
	type RequestBody struct {
		CID int `json:"cid" binding:"min=1,required"`
	}

	var reqBody RequestBody
	if !bindJSONOrAbort(c, &reqBody) {
		return
	}

	claims, ok := requireJwtContext(c)
	if !ok {
		return
	}
	if reqBody.CID != claims.CID && !canAccessUserEditor(claims.NetworkRating) {
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
		PilotRating   int    `json:"pilot_rating"`
	}

	resBody := ResponseBody{
		CID:           user.CID,
		FirstName:     safeStr(user.FirstName),
		LastName:      safeStr(user.LastName),
		NetworkRating: user.NetworkRating,
		PilotRating:   user.PilotRating,
	}

	res := newAPIV1Success(&resBody)
	writeAPIV1Response(c, http.StatusOK, &res)
}

// updateUser updates the user with a specified CID.
//
// The CID itself is immutable and cannot be changed.
// Instructor1+: may set network_rating and pilot_rating up to actor ceilings (any target).
// Supervisor+: may also set name/password when target network rating ≤ actor.
func (s *Server) updateUser(c *gin.Context) {
	claims, ok := requireJwtContext(c)
	if !ok {
		return
	}
	if !canAdjustUserRatings(claims.NetworkRating) {
		writeAPIV1Response(c, http.StatusForbidden, &genericAPIV1Forbidden)
		return
	}

	type RequestBody struct {
		CID           int     `json:"cid" binding:"min=1,required"`
		Password      *string `json:"password"`
		FirstName     *string `json:"first_name"`
		LastName      *string `json:"last_name"`
		NetworkRating *int    `json:"network_rating" binding:"omitempty,min=-1,max=12"`
		PilotRating   *int    `json:"pilot_rating" binding:"omitempty,min=0,max=63"`
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

	actorPilotMax := s.actorPilotRatingCeiling(claims.CID)
	fullOK := canFullMutateTarget(claims.NetworkRating, protocol.NetworkRating(targetUser.NetworkRating))

	// Profile fields require full mutation privilege.
	wantsProfile := reqBody.Password != nil || reqBody.FirstName != nil || reqBody.LastName != nil
	if wantsProfile && !fullOK {
		res := newAPIV1Failure("only supervisors can change name or password (and not on higher-rated users)")
		writeAPIV1Response(c, http.StatusForbidden, &res)
		return
	}

	if fullOK {
		if reqBody.Password != nil {
			targetUser.Password = *reqBody.Password
		}
		if reqBody.FirstName != nil {
			targetUser.FirstName = reqBody.FirstName
		}
		if reqBody.LastName != nil {
			targetUser.LastName = reqBody.LastName
		}
	}

	// Rating ceilings: cannot *raise/change to* above actor's own values (any target).
	// Unchanged higher existing values are allowed when the field is re-sent as-is.
	if reqBody.NetworkRating != nil {
		if *reqBody.NetworkRating > int(claims.NetworkRating) &&
			*reqBody.NetworkRating != targetUser.NetworkRating {
			res := newAPIV1Failure("cannot set network rating above your own")
			writeAPIV1Response(c, http.StatusForbidden, &res)
			return
		}
		targetUser.NetworkRating = *reqBody.NetworkRating
	}
	if reqBody.PilotRating != nil {
		if !isValidPilotRating(*reqBody.PilotRating) {
			res := newAPIV1Failure("invalid pilot rating")
			writeAPIV1Response(c, http.StatusBadRequest, &res)
			return
		}
		if *reqBody.PilotRating > actorPilotMax &&
			*reqBody.PilotRating != targetUser.PilotRating {
			res := newAPIV1Failure("cannot set pilot rating above your own")
			writeAPIV1Response(c, http.StatusForbidden, &res)
			return
		}
		targetUser.PilotRating = *reqBody.PilotRating
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
		PilotRating   int    `json:"pilot_rating"`
	}

	resBody := ResponseBody{
		CID:           targetUser.CID,
		FirstName:     safeStr(targetUser.FirstName),
		LastName:      safeStr(targetUser.LastName),
		NetworkRating: targetUser.NetworkRating,
		PilotRating:   targetUser.PilotRating,
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
		PilotRating   int     `json:"pilot_rating" binding:"omitempty,min=0,max=63"`
	}

	var reqBody RequestBody
	if !bindJSONOrAbort(c, &reqBody) {
		return
	}

	claims, ok := requireJwtContext(c)
	if !ok {
		return
	}
	// Create is full mutation: Supervisor+ only.
	if !canFullMutateUsers(claims.NetworkRating) ||
		reqBody.NetworkRating > int(claims.NetworkRating) {
		writeAPIV1Response(c, http.StatusForbidden, &genericAPIV1Forbidden)
		return
	}
	if !isValidPilotRating(reqBody.PilotRating) {
		res := newAPIV1Failure("invalid pilot rating")
		writeAPIV1Response(c, http.StatusBadRequest, &res)
		return
	}
	actorPilotMax := s.actorPilotRatingCeiling(claims.CID)
	if reqBody.PilotRating > actorPilotMax {
		res := newAPIV1Failure("cannot set pilot rating above your own")
		writeAPIV1Response(c, http.StatusForbidden, &res)
		return
	}

	user := &db.User{
		Password:      reqBody.Password,
		FirstName:     reqBody.FirstName,
		LastName:      reqBody.LastName,
		NetworkRating: reqBody.NetworkRating,
		PilotRating:   reqBody.PilotRating,
	}

	if err := s.dbRepo.UserRepo.CreateUser(user); err != nil {
		writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
		return
	}

	type ResponseBody struct {
		CID           int     `json:"cid"`
		FirstName     *string `json:"first_name"`
		LastName      *string `json:"last_name"`
		NetworkRating int     `json:"network_rating"`
		PilotRating   int     `json:"pilot_rating"`
	}

	resBody := ResponseBody{
		CID:           user.CID,
		FirstName:     user.FirstName,
		LastName:      user.LastName,
		NetworkRating: user.NetworkRating,
		PilotRating:   user.PilotRating,
	}

	res := newAPIV1Success(&resBody)
	writeAPIV1Response(c, http.StatusCreated, &res)
}
