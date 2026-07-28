package web

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/internal/db"
)

// apiUserData is the public user record shape shared by POST /user/load and
// GET /api/v1/users[/:cid]. No password hashes.
type apiUserData struct {
	CID           int    `json:"cid"`
	FirstName     string `json:"first_name"`
	LastName      string `json:"last_name"`
	NetworkRating int    `json:"network_rating"`
	PilotRating   int    `json:"pilot_rating"`
}

func apiUserFromDB(u *db.User) apiUserData {
	return apiUserData{
		CID:           u.CID,
		FirstName:     safeStr(u.FirstName),
		LastName:      safeStr(u.LastName),
		NetworkRating: u.NetworkRating,
		PilotRating:   u.PilotRating,
	}
}

// apiUserListData is the GET /api/v1/users success payload (Stable).
type apiUserListData struct {
	Items    []apiUserData `json:"items"`
	Total    int           `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"page_size"`
	Pages    int           `json:"pages"`
}

// handleAPIListUsers serves GET /api/v1/users — Supervisor+ user directory.
// Query/pagination locked to parseUserDirectoryQuery / clampDirectoryPage
// (docs/design/rest-api-versioning.md §B). Invalid filters never 500.
func (s *Server) handleAPIListUsers(c *gin.Context) {
	claims, ok := requireJwtContext(c)
	if !ok {
		return
	}
	if !canAccessUserEditor(claims.NetworkRating) {
		writeAPIV1Response(c, http.StatusForbidden, &genericAPIV1Forbidden)
		return
	}

	q := parseUserDirectoryQueryAPI(c.Request.URL.Query())

	filter := db.UserListFilter{
		Query:  q.Q,
		Rating: q.Rating,
		Sort:   q.Sort,
		Desc:   q.Desc,
		Limit:  q.PageSize,
		Offset: (q.Page - 1) * q.PageSize,
	}

	total, err := s.dbRepo.UserRepo.CountUsers(filter)
	if err != nil {
		slog.Error("api users list count failed", "err", err)
		writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
		return
	}
	q.Total = total
	clampDirectoryPage(&q)

	// Recompute offset after possible page clamp; echo effective page_size.
	filter.Offset = (q.Page - 1) * q.PageSize
	filter.Limit = q.PageSize

	users, err := s.dbRepo.UserRepo.ListUsers(filter)
	if err != nil {
		slog.Error("api users list failed", "err", err)
		writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
		return
	}

	items := make([]apiUserData, 0, len(users))
	for _, u := range users {
		items = append(items, apiUserFromDB(u))
	}

	data := apiUserListData{
		Items:    items,
		Total:    q.Total,
		Page:     q.Page,
		PageSize: q.PageSize,
		Pages:    q.Pages,
	}
	res := newAPIV1Success(&data)
	writeAPIV1Response(c, http.StatusOK, &res)
}

// handleAPIGetUser serves GET /api/v1/users/:cid — self or Supervisor+.
// Response data matches POST /user/load fields.
func (s *Server) handleAPIGetUser(c *gin.Context) {
	claims, ok := requireJwtContext(c)
	if !ok {
		return
	}

	cid, err := strconv.Atoi(c.Param("cid"))
	if err != nil || cid < 1 {
		res := newAPIV1Failure("invalid cid")
		writeAPIV1Response(c, http.StatusBadRequest, &res)
		return
	}

	if cid != claims.CID && !canAccessUserEditor(claims.NetworkRating) {
		writeAPIV1Response(c, http.StatusForbidden, &genericAPIV1Forbidden)
		return
	}

	user, err := s.dbRepo.UserRepo.GetUserByCID(cid)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeAPIV1Response(c, http.StatusNotFound, &genericAPIV1NotFound)
			return
		}
		slog.Error("api users get failed", "cid", cid, "err", err)
		writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
		return
	}

	data := apiUserFromDB(user)
	res := newAPIV1Success(&data)
	writeAPIV1Response(c, http.StatusOK, &res)
}
