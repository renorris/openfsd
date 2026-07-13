package web

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/internal/db"
	"github.com/renorris/openfsd/pkg/protocol"
)

func (s *Server) newUserEditorPage(c *gin.Context) userEditorPage {
	claims := getJwtContext(c)
	maxRating := int(claims.NetworkRating)
	defaultRating := int(protocol.NetworkRatingObserver)
	if defaultRating > maxRating {
		defaultRating = maxRating
	}
	return userEditorPage{
		basePage: basePage{
			User:      pageUserFromClaims(claims),
			CSRFToken: s.issueCSRFToken(c),
		},
		Create: userForm{
			NetworkRating: defaultRating,
		},
		// Only offer ratings the actor may assign (server still enforces ceiling).
		RatingOptions: ratingOptionsUpTo(maxRating, defaultRating),
	}
}

func actorMaxRating(page *userEditorPage) int {
	if page.User == nil {
		return int(protocol.NetworkRatingObserver)
	}
	return page.User.NetworkRating
}

// handleFrontendUserEditor renders the supervisor user editor.
// GET /usereditor?cid=N loads that user into the edit form (server-rendered).
func (s *Server) handleFrontendUserEditor(c *gin.Context) {
	page := s.newUserEditorPage(c)

	switch c.Query("flash") {
	case "created":
		page.FlashSuccess = "User created successfully"
	case "updated":
		page.FlashSuccess = "User updated successfully"
	}

	if cidStr := strings.TrimSpace(c.Query("cid")); cidStr != "" {
		page.SearchCID = cidStr
		s.loadUserIntoEditForm(&page, cidStr)
	}

	s.writeTemplate(c, "usereditor", page)
}

func (s *Server) loadUserIntoEditForm(page *userEditorPage, cidStr string) {
	cid, err := strconv.Atoi(cidStr)
	if err != nil || cid < 1 {
		page.FlashError = "Enter a valid CID"
		return
	}
	user, err := s.dbRepo.UserRepo.GetUserByCID(cid)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			page.FlashError = "User not found"
			return
		}
		page.FlashError = "Unable to load user"
		return
	}
	page.EditLoaded = true
	page.Edit = userForm{
		CID:           strconv.Itoa(user.CID),
		FirstName:     safeStr(user.FirstName),
		LastName:      safeStr(user.LastName),
		NetworkRating: user.NetworkRating,
	}
	// Keep create/edit selects capped at actor max; selected value is the loaded rating
	// (may appear only via value compare in template if above max — rare for higher targets).
	page.RatingOptions = ratingOptionsUpTo(actorMaxRating(page), user.NetworkRating)
}

// handleFrontendUserCreate processes POST /usereditor/create (no-JS form path).
func (s *Server) handleFrontendUserCreate(c *gin.Context) {
	if !s.validateCSRF(c) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	claims := getJwtContext(c)
	page := s.newUserEditorPage(c)

	firstName := strings.TrimSpace(c.PostForm("first_name"))
	lastName := strings.TrimSpace(c.PostForm("last_name"))
	password := c.PostForm("password")
	ratingStr := c.PostForm("network_rating")

	page.Create.FirstName = firstName
	page.Create.LastName = lastName
	page.Create.Password = "" // never re-render password

	maxRating := int(claims.NetworkRating)
	rating, err := strconv.Atoi(ratingStr)
	if err != nil {
		page.Create.RatingError = "Select a network rating"
		page.Create.NetworkRating = int(protocol.NetworkRatingObserver)
		page.RatingOptions = ratingOptionsUpTo(maxRating, page.Create.NetworkRating)
		s.writeTemplate(c, "usereditor", page)
		return
	}
	page.Create.NetworkRating = rating
	page.RatingOptions = ratingOptionsUpTo(maxRating, rating)

	if len(password) < 8 {
		page.Create.PasswordError = "Password must be at least 8 characters"
		s.writeTemplate(c, "usereditor", page)
		return
	}
	if strings.Contains(password, ":") {
		page.Create.PasswordError = "Password cannot contain colon characters"
		s.writeTemplate(c, "usereditor", page)
		return
	}
	if rating < -1 || rating > 12 {
		page.Create.RatingError = "Invalid network rating"
		s.writeTemplate(c, "usereditor", page)
		return
	}
	if claims.NetworkRating < protocol.NetworkRatingSupervisor || rating > maxRating {
		page.Create.Error = "You cannot create a user with that rating"
		s.writeTemplate(c, "usereditor", page)
		return
	}

	var firstPtr, lastPtr *string
	if firstName != "" {
		firstPtr = &firstName
	}
	if lastName != "" {
		lastPtr = &lastName
	}

	user := &db.User{
		Password:      password,
		FirstName:     firstPtr,
		LastName:      lastPtr,
		NetworkRating: rating,
	}
	if err := s.dbRepo.UserRepo.CreateUser(user); err != nil {
		page.Create.Error = "Unable to create user"
		s.writeTemplate(c, "usereditor", page)
		return
	}

	c.Redirect(http.StatusSeeOther, "/usereditor?cid="+strconv.Itoa(user.CID)+"&flash=created")
}

// handleFrontendUserUpdate processes POST /usereditor/update (no-JS form path).
func (s *Server) handleFrontendUserUpdate(c *gin.Context) {
	if !s.validateCSRF(c) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	claims := getJwtContext(c)
	page := s.newUserEditorPage(c)

	cidStr := strings.TrimSpace(c.PostForm("cid"))
	firstName := strings.TrimSpace(c.PostForm("first_name"))
	lastName := strings.TrimSpace(c.PostForm("last_name"))
	password := c.PostForm("password")
	ratingStr := c.PostForm("network_rating")

	page.EditLoaded = true
	page.Edit = userForm{
		CID:       cidStr,
		FirstName: firstName,
		LastName:  lastName,
	}
	page.SearchCID = cidStr

	cid, err := strconv.Atoi(cidStr)
	if err != nil || cid < 1 {
		page.Edit.CIDError = "Invalid CID"
		s.writeTemplate(c, "usereditor", page)
		return
	}

	maxRating := int(claims.NetworkRating)
	rating, err := strconv.Atoi(ratingStr)
	if err != nil || rating < -1 || rating > 12 {
		page.Edit.RatingError = "Invalid network rating"
		page.Edit.NetworkRating = int(protocol.NetworkRatingObserver)
		page.RatingOptions = ratingOptionsUpTo(maxRating, page.Edit.NetworkRating)
		s.writeTemplate(c, "usereditor", page)
		return
	}
	page.Edit.NetworkRating = rating
	page.RatingOptions = ratingOptionsUpTo(maxRating, rating)

	if password != "" {
		if len(password) < 8 {
			page.Edit.PasswordError = "Password must be at least 8 characters"
			s.writeTemplate(c, "usereditor", page)
			return
		}
		if strings.Contains(password, ":") {
			page.Edit.PasswordError = "Password cannot contain colon characters"
			s.writeTemplate(c, "usereditor", page)
			return
		}
	}

	if claims.NetworkRating < protocol.NetworkRatingSupervisor {
		page.Edit.Error = "Insufficient permission"
		s.writeTemplate(c, "usereditor", page)
		return
	}

	targetUser, err := s.dbRepo.UserRepo.GetUserByCID(cid)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			page.Edit.Error = "User not found"
			s.writeTemplate(c, "usereditor", page)
			return
		}
		page.Edit.Error = "Unable to load user"
		s.writeTemplate(c, "usereditor", page)
		return
	}

	if targetUser.NetworkRating > int(claims.NetworkRating) {
		page.Edit.Error = "Cannot update user with higher network rating"
		s.writeTemplate(c, "usereditor", page)
		return
	}
	if rating > int(claims.NetworkRating) {
		page.Edit.Error = "Cannot set rating above your own"
		s.writeTemplate(c, "usereditor", page)
		return
	}

	var firstPtr, lastPtr *string
	if firstName != "" {
		firstPtr = &firstName
	} else {
		empty := ""
		firstPtr = &empty
	}
	if lastName != "" {
		lastPtr = &lastName
	} else {
		empty := ""
		lastPtr = &empty
	}

	targetUser.FirstName = firstPtr
	targetUser.LastName = lastPtr
	targetUser.NetworkRating = rating
	// Empty password means keep current (UpdateUser contract).
	targetUser.Password = password

	if err := s.dbRepo.UserRepo.UpdateUser(targetUser); err != nil {
		page.Edit.Error = "Unable to update user"
		s.writeTemplate(c, "usereditor", page)
		return
	}

	c.Redirect(http.StatusSeeOther, "/usereditor?cid="+strconv.Itoa(cid)+"&flash=updated")
}
