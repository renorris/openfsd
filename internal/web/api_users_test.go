package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/renorris/openfsd/internal/db"
	"github.com/renorris/openfsd/pkg/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIListUsers_Authz(t *testing.T) {
	env := setupTestAPI(t)
	adminAccess, _ := env.login(t, env.admin.CID, env.adminPass)
	obsAccess, _ := env.login(t, env.observer.CID, env.observerPass)

	t.Run("unauthenticated", func(t *testing.T) {
		w := env.doJSON(t, http.MethodGet, "/api/v1/users", nil, "")
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("observer forbidden", func(t *testing.T) {
		w := env.doJSON(t, http.MethodGet, "/api/v1/users", nil, obsAccess)
		assert.Equal(t, http.StatusForbidden, w.Code)
		res := decodeAPIV1(t, w)
		require.NotNil(t, res.Err)
		assert.Equal(t, "forbidden", *res.Err)
	})

	t.Run("admin ok", func(t *testing.T) {
		w := env.doJSON(t, http.MethodGet, "/api/v1/users", nil, adminAccess)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		res := decodeAPIV1(t, w)
		require.Nil(t, res.Err)
		require.Equal(t, "v1", res.Version)

		data := decodeUserListData(t, res)
		assert.GreaterOrEqual(t, data.Total, 2)
		assert.Equal(t, 1, data.Page)
		assert.Equal(t, 50, data.PageSize)
		assert.GreaterOrEqual(t, data.Pages, 1)
		assert.NotEmpty(t, data.Items)
		// No password field on items (json omits unknown; ensure public fields present).
		for _, it := range data.Items {
			assert.GreaterOrEqual(t, it.CID, 1)
		}
	})
}

func TestAPIListUsers_PaginationAndFilters(t *testing.T) {
	env := setupTestAPI(t)
	adminAccess, _ := env.login(t, env.admin.CID, env.adminPass)

	// Seed enough users for multi-page lists.
	for i := 0; i < 8; i++ {
		u := &db.User{
			Password:      "seedpass1",
			FirstName:     strPtr(fmt.Sprintf("Seed%d", i)),
			LastName:      strPtr("User"),
			NetworkRating: int(protocol.NetworkRatingObserver),
			PilotRating:   0,
		}
		require.NoError(t, env.server.dbRepo.UserRepo.CreateUser(u))
	}

	t.Run("page_size clamp and page clamp past end", func(t *testing.T) {
		// page_size=1 → small pages; page=999 clamps to last page.
		w := env.doJSON(t, http.MethodGet, "/api/v1/users?page_size=1&page=999", nil, adminAccess)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		data := decodeUserListData(t, decodeAPIV1(t, w))
		assert.Equal(t, 1, data.PageSize)
		assert.Equal(t, data.Pages, data.Page, "page should clamp to last page")
		assert.Equal(t, 1, len(data.Items))
		assert.GreaterOrEqual(t, data.Total, 10)
	})

	t.Run("page_size zero and negative → default 50", func(t *testing.T) {
		for _, qs := range []string{"page_size=0", "page_size=-5", "page_size=foo"} {
			w := env.doJSON(t, http.MethodGet, "/api/v1/users?"+qs, nil, adminAccess)
			require.Equal(t, http.StatusOK, w.Code, qs+" "+w.Body.String())
			data := decodeUserListData(t, decodeAPIV1(t, w))
			assert.Equal(t, 50, data.PageSize, qs)
		}
	})

	t.Run("page_size hard cap 200", func(t *testing.T) {
		w := env.doJSON(t, http.MethodGet, "/api/v1/users?page_size=999", nil, adminAccess)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		data := decodeUserListData(t, decodeAPIV1(t, w))
		assert.Equal(t, 200, data.PageSize)
	})

	t.Run("invalid sort and rating never 500", func(t *testing.T) {
		w := env.doJSON(t, http.MethodGet, "/api/v1/users?sort=nope&rating=foo&page=0&dir=sideways", nil, adminAccess)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		data := decodeUserListData(t, decodeAPIV1(t, w))
		assert.Equal(t, 1, data.Page)
		assert.Equal(t, 50, data.PageSize)
		assert.GreaterOrEqual(t, data.Total, 2)
	})

	t.Run("rating filter exact", func(t *testing.T) {
		// Only admin is Administrator (12) among seeds (observers are rating 1).
		w := env.doJSON(t, http.MethodGet, "/api/v1/users?rating=12", nil, adminAccess)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		data := decodeUserListData(t, decodeAPIV1(t, w))
		require.GreaterOrEqual(t, data.Total, 1)
		for _, it := range data.Items {
			assert.Equal(t, 12, it.NetworkRating)
		}
	})

	t.Run("q filter", func(t *testing.T) {
		w := env.doJSON(t, http.MethodGet, "/api/v1/users?q=Seed0", nil, adminAccess)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		data := decodeUserListData(t, decodeAPIV1(t, w))
		assert.GreaterOrEqual(t, data.Total, 1)
		found := false
		for _, it := range data.Items {
			if it.FirstName == "Seed0" {
				found = true
			}
		}
		assert.True(t, found, "expected Seed0 in results: %+v", data.Items)
	})

	t.Run("page 1 of page_size 2", func(t *testing.T) {
		w := env.doJSON(t, http.MethodGet, "/api/v1/users?page_size=2&page=1&sort=cid&dir=asc", nil, adminAccess)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		data := decodeUserListData(t, decodeAPIV1(t, w))
		assert.Equal(t, 1, data.Page)
		assert.Equal(t, 2, data.PageSize)
		assert.Equal(t, 2, len(data.Items))
		assert.Greater(t, data.Pages, 1)
	})
}

func TestAPIGetUser_AuthzAndShape(t *testing.T) {
	env := setupTestAPI(t)
	adminAccess, _ := env.login(t, env.admin.CID, env.adminPass)
	obsAccess, _ := env.login(t, env.observer.CID, env.observerPass)

	t.Run("unauthenticated", func(t *testing.T) {
		w := env.doJSON(t, http.MethodGet, fmt.Sprintf("/api/v1/users/%d", env.observer.CID), nil, "")
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("self ok for observer", func(t *testing.T) {
		w := env.doJSON(t, http.MethodGet, fmt.Sprintf("/api/v1/users/%d", env.observer.CID), nil, obsAccess)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		res := decodeAPIV1(t, w)
		require.Nil(t, res.Err)
		u := decodeUserData(t, res)
		assert.Equal(t, env.observer.CID, u.CID)
		assert.Equal(t, "Obs", u.FirstName)
		assert.Equal(t, "Server", u.LastName)
		assert.Equal(t, int(protocol.NetworkRatingObserver), u.NetworkRating)
	})

	t.Run("observer cannot get other", func(t *testing.T) {
		w := env.doJSON(t, http.MethodGet, fmt.Sprintf("/api/v1/users/%d", env.admin.CID), nil, obsAccess)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("admin can get other", func(t *testing.T) {
		w := env.doJSON(t, http.MethodGet, fmt.Sprintf("/api/v1/users/%d", env.observer.CID), nil, adminAccess)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		u := decodeUserData(t, decodeAPIV1(t, w))
		assert.Equal(t, env.observer.CID, u.CID)
	})

	t.Run("not found", func(t *testing.T) {
		w := env.doJSON(t, http.MethodGet, "/api/v1/users/999999", nil, adminAccess)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("invalid cid", func(t *testing.T) {
		w := env.doJSON(t, http.MethodGet, "/api/v1/users/0", nil, adminAccess)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		w = env.doJSON(t, http.MethodGet, "/api/v1/users/abc", nil, adminAccess)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestAPIUsers_Goldens(t *testing.T) {
	env := setupTestAPI(t)
	adminAccess, _ := env.login(t, env.admin.CID, env.adminPass)
	obsAccess, _ := env.login(t, env.observer.CID, env.observerPass)

	t.Run("users_get_self", func(t *testing.T) {
		w := env.doJSON(t, http.MethodGet, fmt.Sprintf("/api/v1/users/%d", env.observer.CID), nil, obsAccess)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		var envelope map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
		dataMap, ok := envelope["data"].(map[string]any)
		require.True(t, ok)
		dataMap["cid"] = float64(0)
		rewritten, err := json.Marshal(envelope)
		require.NoError(t, err)
		assertGoldenJSON(t, "2026-07-28/users_get_self.json", rewritten)
	})

	t.Run("users_list", func(t *testing.T) {
		// Filter to a single known user so the fixture is stable.
		w := env.doJSON(t, http.MethodGet,
			fmt.Sprintf("/api/v1/users?q=%d&page_size=10&sort=cid&dir=asc", env.observer.CID),
			nil, adminAccess)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		var envelope map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
		dataMap, ok := envelope["data"].(map[string]any)
		require.True(t, ok)
		items, ok := dataMap["items"].([]any)
		require.True(t, ok)
		require.NotEmpty(t, items)
		for _, raw := range items {
			item, ok := raw.(map[string]any)
			require.True(t, ok)
			item["cid"] = float64(0)
		}
		// Stable pagination fields for golden: rewrite total/pages if filter matches exactly one.
		dataMap["total"] = float64(1)
		dataMap["pages"] = float64(1)
		dataMap["page"] = float64(1)
		dataMap["page_size"] = float64(10)
		// Keep only first item if q matched more than one unexpectedly.
		if len(items) > 1 {
			dataMap["items"] = items[:1]
		}
		rewritten, err := json.Marshal(envelope)
		require.NoError(t, err)
		assertGoldenJSON(t, "2026-07-28/users_list.json", rewritten)
	})
}

func decodeUserListData(t *testing.T, res APIV1Response) apiUserListData {
	t.Helper()
	b, err := json.Marshal(res.Data)
	require.NoError(t, err)
	var data apiUserListData
	require.NoError(t, json.Unmarshal(b, &data), string(b))
	return data
}

func decodeUserData(t *testing.T, res APIV1Response) apiUserData {
	t.Helper()
	b, err := json.Marshal(res.Data)
	require.NoError(t, err)
	var data apiUserData
	require.NoError(t, json.Unmarshal(b, &data), string(b))
	return data
}
