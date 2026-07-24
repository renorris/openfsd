package web

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
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
	actorPilotMax := s.actorPilotRatingCeiling(claims.CID)
	defaultPilot := 0
	if defaultPilot > actorPilotMax {
		defaultPilot = actorPilotMax
	}
	return userEditorPage{
		basePage: basePage{
			User:      pageUserFromClaims(claims),
			CSRFToken: s.issueCSRFToken(c),
		},
		Create: userForm{
			NetworkRating: defaultRating,
			PilotRating:   defaultPilot,
		},
		// Only offer ratings the actor may assign (server still enforces ceiling).
		RatingOptions:      ratingOptionsUpTo(maxRating, defaultRating),
		PilotRatingOptions: pilotRatingOptionsUpTo(actorPilotMax, defaultPilot),
	}
}

func actorMaxRating(page *userEditorPage) int {
	if page.User == nil {
		return int(protocol.NetworkRatingObserver)
	}
	return page.User.NetworkRating
}

// actorPilotRatingCeiling loads the actor's stored pilot_rating (VATSIM scale).
// Invalid stored values fall back to the highest official rating at or below
// the stored number (or P0).
func (s *Server) actorPilotRatingCeiling(cid int) int {
	u, err := s.dbRepo.UserRepo.GetUserByCID(cid)
	if err != nil || u == nil {
		return int(protocol.PilotRatingNone)
	}
	if protocol.IsValidPilotRating(u.PilotRating) {
		return u.PilotRating
	}
	return maxValidPilotRatingAtMost(u.PilotRating)
}

// loadUserDirectory fills Dir totals/pages and Users rows for the current query.
// On DB failure sets FlashError and logs; still leaves a renderable page.
func (s *Server) loadUserDirectory(page *userEditorPage, selectedCID int) {
	filter := db.UserListFilter{
		Query:  page.Dir.Q,
		Rating: page.Dir.Rating,
		Sort:   page.Dir.Sort,
		Desc:   page.Dir.Desc,
		Limit:  page.Dir.PageSize,
		Offset: (page.Dir.Page - 1) * page.Dir.PageSize,
	}

	total, err := s.dbRepo.UserRepo.CountUsers(filter)
	if err != nil {
		slog.Error("user directory count failed", "err", err)
		page.FlashError = "Unable to load user directory"
		return
	}
	page.Dir.Total = total
	clampDirectoryPage(&page.Dir)

	// Recompute offset after possible page clamp.
	filter.Offset = (page.Dir.Page - 1) * page.Dir.PageSize
	filter.Limit = page.Dir.PageSize

	users, err := s.dbRepo.UserRepo.ListUsers(filter)
	if err != nil {
		slog.Error("user directory list failed", "err", err)
		page.FlashError = "Unable to load user directory"
		return
	}

	base := directoryValuesFromQuery(page.Dir)
	rows := make([]userDirectoryRow, 0, len(users))
	for _, u := range users {
		extras := url.Values{}
		extras.Set("cid", strconv.Itoa(u.CID))
		rows = append(rows, userDirectoryRow{
			CID:         u.CID,
			DisplayName: userDisplayName(u.FirstName, u.LastName, u.CID),
			Rating:      u.NetworkRating,
			RatingShort: networkRatingShort(u.NetworkRating),
			RatingLabel: networkRatingLabel(u.NetworkRating),
			PilotRating: u.PilotRating,
			PilotShort:  pilotRatingShort(u.PilotRating),
			PilotLabel:  pilotRatingLabel(u.PilotRating),
			Selected:    selectedCID >= 1 && u.CID == selectedCID,
			EditHref:    directoryHref(base, extras),
		})
	}
	page.Users = rows
}

// attachDirectoryChrome sets filter options, sort/pager/new-user hrefs.
func attachDirectoryChrome(page *userEditorPage, selectedCID int) {
	page.FilterRatingOptions = filterRatingOptions(page.Dir.Rating)
	page.SortCIDHref = sortHref(page.Dir, "cid", selectedCID)
	page.SortNameHref = sortHref(page.Dir, "name", selectedCID)
	page.SortRatingHref = sortHref(page.Dir, "rating", selectedCID)

	baseNoPage := directoryValuesFromQuery(page.Dir)
	// New user: directory params + new=1, without cid/flash.
	newExtras := url.Values{}
	newExtras.Set("new", "1")
	// Drop page from new-user? Keep page so user returns to same list context.
	page.NewUserHref = directoryHref(baseNoPage, newExtras)

	if page.Dir.Page > 1 {
		page.PrevPageHref = pageHref(page.Dir, page.Dir.Page-1, selectedCID)
	}
	if page.Dir.Page < page.Dir.Pages {
		page.NextPageHref = pageHref(page.Dir, page.Dir.Page+1, selectedCID)
	}
}

// handleFrontendUserEditor renders the supervisor user directory + create/edit rail.
// GET /usereditor?q&rating&sort&dir&page&cid&new&flash
func (s *Server) handleFrontendUserEditor(c *gin.Context) {
	page := s.newUserEditorPage(c)
	page.Dir = parseUserDirectoryQuery(c.Request.URL.Query())

	switch c.Query("flash") {
	case "created":
		page.FlashSuccess = "User created successfully"
	case "updated":
		page.FlashSuccess = "User updated successfully"
	}

	cidStr := strings.TrimSpace(c.Query("cid"))
	wantNew := c.Query("new") == "1"
	selectedCID := 0
	if cidStr != "" {
		if cid, err := strconv.Atoi(cidStr); err == nil && cid >= 1 {
			selectedCID = cid
		}
	}

	// Rail state: cid set → edit wins over new; neither → empty.
	// Create is Supervisor+ only (full mutation).
	if selectedCID >= 1 {
		s.loadUserIntoEditForm(&page, strconv.Itoa(selectedCID))
		// loadUserIntoEditForm sets EditLoaded on success; on not-found leaves empty rail.
		page.ShowCreate = false
	} else if wantNew && page.User != nil && page.User.CanFullMutateUsers {
		page.ShowCreate = true
		page.EditLoaded = false
	}

	s.loadUserDirectory(&page, selectedCID)
	attachDirectoryChrome(&page, selectedCID)

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
		slog.Error("user load failed", "cid", cid, "err", err)
		page.FlashError = "Unable to load user"
		return
	}
	page.EditLoaded = true
	actorMax := actorMaxRating(page)
	actorPilotMax := int(protocol.PilotRatingNone)
	if page.User != nil {
		actorPilotMax = s.actorPilotRatingCeiling(page.User.CID)
	}
	// Profile (name/password): SUP+ and target network rating ≤ actor.
	// Ratings: I1+ may always adjust within ceilings (any target).
	fullOK := page.User != nil && canFullMutateTarget(
		protocol.NetworkRating(page.User.NetworkRating),
		protocol.NetworkRating(user.NetworkRating),
	)
	page.ProfileLocked = !fullOK
	page.RatingsLocked = page.User == nil || !page.User.CanAdjustRatings
	page.EditReadOnly = page.ProfileLocked && page.RatingsLocked
	page.Edit = userForm{
		CID:           strconv.Itoa(user.CID),
		FirstName:     safeStr(user.FirstName),
		LastName:      safeStr(user.LastName),
		NetworkRating: user.NetworkRating,
		PilotRating:   user.PilotRating,
	}
	// Network select capped at actor max. If target is currently above actor,
	// still show their rating as a selected option (cannot re-select higher).
	page.RatingOptions = ratingOptionsUpTo(actorMax, user.NetworkRating)
	if user.NetworkRating > actorMax {
		found := false
		for _, o := range page.RatingOptions {
			if o.Value == user.NetworkRating {
				found = true
				break
			}
		}
		if !found {
			page.RatingOptions = append(page.RatingOptions, ratingOption{
				Value:    user.NetworkRating,
				Label:    networkRatingLabel(user.NetworkRating),
				Selected: true,
			})
		}
	}
	page.PilotRatingOptions = pilotRatingOptionsUpTo(actorPilotMax, user.PilotRating)
	if user.PilotRating > actorPilotMax {
		found := false
		for _, o := range page.PilotRatingOptions {
			if o.Value == user.PilotRating {
				found = true
				break
			}
		}
		if !found {
			page.PilotRatingOptions = append(page.PilotRatingOptions, ratingOption{
				Value:    user.PilotRating,
				Label:    pilotRatingLabel(user.PilotRating),
				Selected: true,
			})
		}
	}
}

// reRenderUserEditor reloads directory + chrome after a validation failure on POST.
func (s *Server) reRenderUserEditor(c *gin.Context, page *userEditorPage, dir url.Values) {
	page.Dir = parseUserDirectoryQuery(dir)
	selectedCID := 0
	if page.EditLoaded {
		if cid, err := strconv.Atoi(page.Edit.CID); err == nil {
			selectedCID = cid
		}
	}
	s.loadUserDirectory(page, selectedCID)
	attachDirectoryChrome(page, selectedCID)
	s.writeTemplate(c, "usereditor", page)
}

// handleFrontendUserCreate processes POST /usereditor/create (no-JS form path).
// Supervisor+ only (full mutation).
func (s *Server) handleFrontendUserCreate(c *gin.Context) {
	if !s.validateCSRF(c) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	claims := getJwtContext(c)
	page := s.newUserEditorPage(c)
	page.ShowCreate = true
	// Ensure form is parsed before reading PostForm map for dir_* fields.
	_ = c.Request.ParseForm()
	dir := directoryValuesFromPost(c.Request.PostForm)

	if !canFullMutateUsers(claims.NetworkRating) {
		page.ShowCreate = false
		page.FlashError = "Only supervisors can create users"
		s.reRenderUserEditor(c, &page, dir)
		return
	}

	firstName := strings.TrimSpace(c.PostForm("first_name"))
	lastName := strings.TrimSpace(c.PostForm("last_name"))
	password := c.PostForm("password")
	ratingStr := c.PostForm("network_rating")
	pilotStr := c.PostForm("pilot_rating")

	page.Create.FirstName = firstName
	page.Create.LastName = lastName
	page.Create.Password = "" // never re-render password

	maxRating := int(claims.NetworkRating)
	actorPilotMax := s.actorPilotRatingCeiling(claims.CID)
	rating, err := strconv.Atoi(ratingStr)
	if err != nil {
		page.Create.RatingError = "Select a network rating"
		page.Create.NetworkRating = int(protocol.NetworkRatingObserver)
		page.RatingOptions = ratingOptionsUpTo(maxRating, page.Create.NetworkRating)
		page.PilotRatingOptions = pilotRatingOptionsUpTo(actorPilotMax, 0)
		s.reRenderUserEditor(c, &page, dir)
		return
	}
	page.Create.NetworkRating = rating
	page.RatingOptions = ratingOptionsUpTo(maxRating, rating)

	pilotRating := int(protocol.PilotRatingNone)
	if pilotStr != "" {
		pilotRating, err = strconv.Atoi(pilotStr)
		if err != nil || !isValidPilotRating(pilotRating) {
			page.Create.PilotError = "Invalid pilot rating"
			page.PilotRatingOptions = pilotRatingOptionsUpTo(actorPilotMax, 0)
			s.reRenderUserEditor(c, &page, dir)
			return
		}
	}
	page.Create.PilotRating = pilotRating
	page.PilotRatingOptions = pilotRatingOptionsUpTo(actorPilotMax, pilotRating)

	if len(password) < 8 {
		page.Create.PasswordError = "Password must be at least 8 characters"
		s.reRenderUserEditor(c, &page, dir)
		return
	}
	if strings.Contains(password, ":") {
		page.Create.PasswordError = "Password cannot contain colon characters"
		s.reRenderUserEditor(c, &page, dir)
		return
	}
	if rating < -1 || rating > 12 {
		page.Create.RatingError = "Invalid network rating"
		s.reRenderUserEditor(c, &page, dir)
		return
	}
	if rating > maxRating {
		page.Create.Error = "You cannot create a user with that network rating"
		s.reRenderUserEditor(c, &page, dir)
		return
	}
	if pilotRating > actorPilotMax {
		page.Create.PilotError = "Cannot set pilot rating above your own"
		s.reRenderUserEditor(c, &page, dir)
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
		PilotRating:   pilotRating,
	}
	if err := s.dbRepo.UserRepo.CreateUser(user); err != nil {
		slog.Error("user create failed", "err", err)
		page.Create.Error = "Unable to create user"
		s.reRenderUserEditor(c, &page, dir)
		return
	}

	c.Redirect(http.StatusSeeOther, userEditorRedirect(dir, user.CID, "created"))
}

// handleFrontendUserUpdate processes POST /usereditor/update (no-JS form path).
//
// Instructor1+: may set network_rating and pilot_rating up to actor ceilings on any user.
// Supervisor+: may also mutate name/password when target network rating ≤ actor.
func (s *Server) handleFrontendUserUpdate(c *gin.Context) {
	if !s.validateCSRF(c) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	claims := getJwtContext(c)
	page := s.newUserEditorPage(c)
	_ = c.Request.ParseForm()
	dir := directoryValuesFromPost(c.Request.PostForm)

	cidStr := strings.TrimSpace(c.PostForm("cid"))
	firstName := strings.TrimSpace(c.PostForm("first_name"))
	lastName := strings.TrimSpace(c.PostForm("last_name"))
	password := c.PostForm("password")
	ratingStr := c.PostForm("network_rating")
	pilotStr := c.PostForm("pilot_rating")

	page.EditLoaded = true
	page.ShowCreate = false
	page.Edit = userForm{
		CID:       cidStr,
		FirstName: firstName,
		LastName:  lastName,
	}

	cid, err := strconv.Atoi(cidStr)
	if err != nil || cid < 1 {
		page.Edit.CIDError = "Invalid CID"
		s.reRenderUserEditor(c, &page, dir)
		return
	}

	if !canAdjustUserRatings(claims.NetworkRating) {
		page.Edit.Error = "Insufficient permission"
		s.reRenderUserEditor(c, &page, dir)
		return
	}

	maxRating := int(claims.NetworkRating)
	actorPilotMax := s.actorPilotRatingCeiling(claims.CID)
	rating, err := strconv.Atoi(ratingStr)
	if err != nil || rating < -1 || rating > 12 {
		page.Edit.RatingError = "Invalid network rating"
		page.Edit.NetworkRating = int(protocol.NetworkRatingObserver)
		page.RatingOptions = ratingOptionsUpTo(maxRating, page.Edit.NetworkRating)
		page.PilotRatingOptions = pilotRatingOptionsUpTo(actorPilotMax, 0)
		s.reRenderUserEditor(c, &page, dir)
		return
	}
	page.Edit.NetworkRating = rating
	page.RatingOptions = ratingOptionsUpTo(maxRating, rating)

	pilotRating := int(protocol.PilotRatingNone)
	if pilotStr != "" {
		pilotRating, err = strconv.Atoi(pilotStr)
		if err != nil || !isValidPilotRating(pilotRating) {
			page.Edit.PilotError = "Invalid pilot rating"
			page.Edit.PilotRating = int(protocol.PilotRatingNone)
			page.PilotRatingOptions = pilotRatingOptionsUpTo(actorPilotMax, 0)
			s.reRenderUserEditor(c, &page, dir)
			return
		}
	}
	page.Edit.PilotRating = pilotRating
	page.PilotRatingOptions = pilotRatingOptionsUpTo(actorPilotMax, pilotRating)

	if password != "" {
		if len(password) < 8 {
			page.Edit.PasswordError = "Password must be at least 8 characters"
			s.reRenderUserEditor(c, &page, dir)
			return
		}
		if strings.Contains(password, ":") {
			page.Edit.PasswordError = "Password cannot contain colon characters"
			s.reRenderUserEditor(c, &page, dir)
			return
		}
	}

	targetUser, err := s.dbRepo.UserRepo.GetUserByCID(cid)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			page.Edit.Error = "User not found"
			s.reRenderUserEditor(c, &page, dir)
			return
		}
		slog.Error("user update load failed", "cid", cid, "err", err)
		page.Edit.Error = "Unable to load user"
		s.reRenderUserEditor(c, &page, dir)
		return
	}

	// Lock flags for re-render on validation errors after load.
	fullOK := canFullMutateTarget(claims.NetworkRating, protocol.NetworkRating(targetUser.NetworkRating))
	page.ProfileLocked = !fullOK
	page.RatingsLocked = false
	page.EditReadOnly = page.ProfileLocked && page.RatingsLocked
	page.Edit.FirstName = safeStr(targetUser.FirstName)
	page.Edit.LastName = safeStr(targetUser.LastName)
	// Keep submitted names in form when full mutate is allowed so validation re-shows them.
	if fullOK {
		page.Edit.FirstName = firstName
		page.Edit.LastName = lastName
	}

	// Ceilings apply when *changing* a rating. Leaving a higher existing value
	// unchanged is allowed so pilot/network edits can be independent.
	if rating > maxRating && rating != targetUser.NetworkRating {
		page.Edit.Error = "Cannot set network rating above your own"
		s.reRenderUserEditor(c, &page, dir)
		return
	}
	if pilotRating > actorPilotMax && pilotRating != targetUser.PilotRating {
		page.Edit.PilotError = "Cannot set pilot rating above your own"
		s.reRenderUserEditor(c, &page, dir)
		return
	}

	// Ratings: any target; new values must not exceed actor ceilings.
	targetUser.NetworkRating = rating
	targetUser.PilotRating = pilotRating

	// Profile fields: SUP+ only and target network rating ≤ actor.
	if fullOK {
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
		// Empty password means keep current (UpdateUser contract).
		targetUser.Password = password
	} else {
		// Rating-only path: never change name/password; reject attempts that look like full mutate.
		if password != "" {
			page.Edit.Error = "Only supervisors can change passwords"
			page.Edit.FirstName = safeStr(targetUser.FirstName)
			page.Edit.LastName = safeStr(targetUser.LastName)
			s.reRenderUserEditor(c, &page, dir)
			return
		}
		// Leave FirstName/LastName/Password as loaded (empty Password = no hash change).
		targetUser.Password = ""
	}

	if err := s.dbRepo.UserRepo.UpdateUser(targetUser); err != nil {
		slog.Error("user update failed", "cid", cid, "err", err)
		page.Edit.Error = "Unable to update user"
		s.reRenderUserEditor(c, &page, dir)
		return
	}

	c.Redirect(http.StatusSeeOther, userEditorRedirect(dir, cid, "updated"))
}
