package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/internal/auth"
	"github.com/renorris/openfsd/pkg/protocol"
)

func TestGetJwtContext_EmptyReturnsNil(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)

	if got := getJwtContext(c); got != nil {
		t.Fatalf("getJwtContext empty = %v, want nil", got)
	}
}

func TestGetJwtContext_WrongTypeReturnsNil(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)
	c.Set(jwtContextKey, "not-claims")

	if got := getJwtContext(c); got != nil {
		t.Fatalf("getJwtContext wrong type = %v, want nil", got)
	}
}

func TestGetJwtContext_Present(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)
	want := &auth.CustomClaims{CustomFields: auth.CustomFields{
		CID: 42, NetworkRating: protocol.NetworkRatingObserver,
	}}
	setJwtContext(c, want)

	got := getJwtContext(c)
	if got == nil || got.CID != 42 {
		t.Fatalf("getJwtContext = %#v, want CID 42", got)
	}
}

func TestRequireJwtContext_MissingAborts500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/dashboard", nil)

	claims, ok := requireJwtContext(c)
	if ok || claims != nil {
		t.Fatalf("requireJwtContext missing = (%v, %v), want (nil, false)", claims, ok)
	}
	if !c.IsAborted() {
		t.Fatal("expected context aborted")
	}
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestRequireJwtContext_Present(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	want := &auth.CustomClaims{CustomFields: auth.CustomFields{
		CID: 7, NetworkRating: protocol.NetworkRatingAdministator,
	}}
	setJwtContext(c, want)

	claims, ok := requireJwtContext(c)
	if !ok || claims == nil || claims.CID != 7 {
		t.Fatalf("requireJwtContext = (%#v, %v)", claims, ok)
	}
	if c.IsAborted() {
		t.Fatal("must not abort when claims present")
	}
}
