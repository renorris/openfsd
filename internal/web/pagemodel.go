package web

import (
	"fmt"
	"strings"

	"github.com/renorris/openfsd/internal/auth"
	"github.com/renorris/openfsd/pkg/protocol"
)

// pageUser is the authenticated user projection for HTML templates.
type pageUser struct {
	CID                int
	DisplayName        string
	FirstName          string
	LastName           string
	NetworkRating      int
	NetworkRatingLabel string
	CanEditUsers       bool
	CanEditConfig      bool
}

// basePage is embedded by every HTML page model so layout has nav data.
type basePage struct {
	User      *pageUser
	CSRFToken string
}

type loginPage struct {
	basePage
	CID        string
	RememberMe bool
	Error      string
	CIDError   string
	PassError  string
}

type dashboardPage struct {
	basePage
}

type editorPage struct {
	basePage
}

func pageUserFromClaims(claims *auth.CustomClaims) *pageUser {
	if claims == nil {
		return nil
	}
	display := strings.TrimSpace(claims.FirstName + " " + claims.LastName)
	if display == "" {
		display = fmt.Sprintf("CID %d", claims.CID)
	}
	rating := int(claims.NetworkRating)
	return &pageUser{
		CID:                claims.CID,
		DisplayName:        display,
		FirstName:          claims.FirstName,
		LastName:           claims.LastName,
		NetworkRating:      rating,
		NetworkRatingLabel: networkRatingLabel(rating),
		CanEditUsers:       claims.NetworkRating >= protocol.NetworkRatingSupervisor,
		CanEditConfig:      claims.NetworkRating >= protocol.NetworkRatingAdministator,
	}
}

func networkRatingLabel(val int) string {
	switch protocol.NetworkRating(val) {
	case protocol.NetworkRatingInactive:
		return "Inactive"
	case protocol.NetworkRatingSuspended:
		return "Suspended"
	case protocol.NetworkRatingObserver:
		return "Observer"
	case protocol.NetworkRatingStudent1:
		return "Student 1"
	case protocol.NetworkRatingStudent2:
		return "Student 2"
	case protocol.NetworkRatingStudent3:
		return "Student 3"
	case protocol.NetworkRatingController1:
		return "Controller 1"
	case protocol.NetworkRatingController2:
		return "Controller 2"
	case protocol.NetworkRatingController3:
		return "Controller 3"
	case protocol.NetworkRatingInstructor1:
		return "Instructor 1"
	case protocol.NetworkRatingInstructor2:
		return "Instructor 2"
	case protocol.NetworkRatingInstructor3:
		return "Instructor 3"
	case protocol.NetworkRatingSupervisor:
		return "Supervisor"
	case protocol.NetworkRatingAdministator:
		return "Administrator"
	default:
		return "Unknown"
	}
}
