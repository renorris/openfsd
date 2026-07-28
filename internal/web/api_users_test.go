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

	// Critical SUP+ boundary: Instructor1 forbidden; true Supervisor allowed.
	i1Pass := "i1pass1234"
	i1 := &db.User{
		Password:      i1Pass,
		FirstName:     strPtr("Inst"),
		LastName:      strPtr("One"),
		NetworkRating: int(protocol.NetworkRatingInstructor1),
	}
	require.NoError(t, env.server.dbRepo.UserRepo.CreateUser(i1))
	i1Access, _ := env.login(t, i1.CID, i1Pass)

	supPass := "suppass123"
	sup := &db.User{
		Password:      supPass,
		FirstName:     strPtr("Super"),
		LastName:      strPtr("Visor"),
		NetworkRating: int(protocol.NetworkRatingSupervisor),
	}
	require.NoError(t, env.server.dbRepo.UserRepo.CreateUser(sup))
	supAccess, _ := env.login(t, sup.CID, supPass)

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

	t.Run("instructor1 forbidden", func(t *testing.T) {
		w := env.doJSON(t, http.MethodGet, "/api/v1/users", nil, i1Access)
		assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	})

	t.Run("supervisor ok", func(t *testing.T) {
		w := env.doJSON(t, http.MethodGet, "/api/v1/users", nil, supAccess)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		res := decodeAPIV1(t, w)
		require.Nil(t, res.Err)
		data := decodeUserListData(t, res)
		assert.GreaterOrEqual(t, data.Total, 2)
		assert.Equal(t, 1, data.Page)
		assert.Equal(t, 50, data.PageSize)
		assert.NotEmpty(t, data.Items)
		assertItemsNoPassword(t, w.Body.Bytes())
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
		assertItemsNoPassword(t, w.Body.Bytes())
	})
}

func TestAPIListUsers_PaginationAndFilters(t *testing.T) {
	env := setupTestAPI(t)
	adminAccess, _ := env.login(t, env.admin.CID, env.adminPass)

	// Seed named users for multi-page lists and sort-order checks.
	// Names chosen so first_name order is Charlie < Alpha is wrong → Alpha, Bravo, Charlie.
	type seed struct {
		first, last string
	}
	seeds := []seed{
		{"Alpha", "User"},
		{"Bravo", "User"},
		{"Charlie", "User"},
		{"Delta", "User"},
		{"Echo", "User"},
		{"Foxtrot", "User"},
		{"Golf", "User"},
		{"Hotel", "User"},
	}
	for _, s := range seeds {
		u := &db.User{
			Password:      "seedpass1",
			FirstName:     strPtr(s.first),
			LastName:      strPtr(s.last),
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

	t.Run("empty result envelope", func(t *testing.T) {
		w := env.doJSON(t, http.MethodGet, "/api/v1/users?q=zzznomatch999&page=5&page_size=25", nil, adminAccess)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		res := decodeAPIV1(t, w)
		require.Nil(t, res.Err)

		// Raw envelope: items must be [] not null.
		var envelope map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
		dataMap, ok := envelope["data"].(map[string]any)
		require.True(t, ok)
		items, ok := dataMap["items"].([]any)
		require.True(t, ok, "items must be a JSON array, got %T", dataMap["items"])
		assert.Empty(t, items)

		data := decodeUserListData(t, res)
		assert.Equal(t, 0, data.Total)
		assert.Equal(t, 1, data.Page, "page clamps to 1 when total=0")
		assert.Equal(t, 1, data.Pages)
		assert.Equal(t, 25, data.PageSize)
		assert.NotNil(t, data.Items)
		assert.Empty(t, data.Items)
	})

	t.Run("sort cid desc order", func(t *testing.T) {
		w := env.doJSON(t, http.MethodGet, "/api/v1/users?sort=cid&dir=desc&page_size=5", nil, adminAccess)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		data := decodeUserListData(t, decodeAPIV1(t, w))
		require.GreaterOrEqual(t, len(data.Items), 2)
		for i := 1; i < len(data.Items); i++ {
			assert.GreaterOrEqual(t, data.Items[i-1].CID, data.Items[i].CID,
				"cid desc: items[%d].CID=%d items[%d].CID=%d", i-1, data.Items[i-1].CID, i, data.Items[i].CID)
		}
	})

	t.Run("sort name asc order", func(t *testing.T) {
		// Filter to seeded *User last names so Admin/Obs do not interleave unpredictably.
		w := env.doJSON(t, http.MethodGet, "/api/v1/users?q=User&sort=name&dir=asc&page_size=20", nil, adminAccess)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		data := decodeUserListData(t, decodeAPIV1(t, w))
		require.GreaterOrEqual(t, len(data.Items), 3)
		for i := 1; i < len(data.Items); i++ {
			prev := data.Items[i-1].FirstName + " " + data.Items[i-1].LastName
			cur := data.Items[i].FirstName + " " + data.Items[i].LastName
			assert.LessOrEqual(t, prev, cur, "name asc order broken at %d: %q > %q", i, prev, cur)
		}
	})

	t.Run("invalid sort falls back to cid order", func(t *testing.T) {
		wNope := env.doJSON(t, http.MethodGet, "/api/v1/users?sort=nope&dir=asc&page_size=10", nil, adminAccess)
		wCid := env.doJSON(t, http.MethodGet, "/api/v1/users?sort=cid&dir=asc&page_size=10", nil, adminAccess)
		require.Equal(t, http.StatusOK, wNope.Code)
		require.Equal(t, http.StatusOK, wCid.Code)
		nope := decodeUserListData(t, decodeAPIV1(t, wNope))
		cid := decodeUserListData(t, decodeAPIV1(t, wCid))
		require.Equal(t, len(cid.Items), len(nope.Items))
		for i := range cid.Items {
			assert.Equal(t, cid.Items[i].CID, nope.Items[i].CID, "index %d", i)
		}
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
		w := env.doJSON(t, http.MethodGet, "/api/v1/users?q=Alpha", nil, adminAccess)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		data := decodeUserListData(t, decodeAPIV1(t, w))
		assert.GreaterOrEqual(t, data.Total, 1)
		found := false
		for _, it := range data.Items {
			if it.FirstName == "Alpha" {
				found = true
			}
		}
		assert.True(t, found, "expected Alpha in results: %+v", data.Items)
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

	i1Pass := "i1pass1234"
	i1 := &db.User{
		Password:      i1Pass,
		FirstName:     strPtr("Inst"),
		LastName:      strPtr("One"),
		NetworkRating: int(protocol.NetworkRatingInstructor1),
	}
	require.NoError(t, env.server.dbRepo.UserRepo.CreateUser(i1))
	i1Access, _ := env.login(t, i1.CID, i1Pass)

	supPass := "suppass123"
	sup := &db.User{
		Password:      supPass,
		FirstName:     strPtr("Super"),
		LastName:      strPtr("Visor"),
		NetworkRating: int(protocol.NetworkRatingSupervisor),
	}
	require.NoError(t, env.server.dbRepo.UserRepo.CreateUser(sup))
	supAccess, _ := env.login(t, sup.CID, supPass)

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
		assertGetNoPassword(t, w.Body.Bytes())
	})

	t.Run("observer cannot get other", func(t *testing.T) {
		w := env.doJSON(t, http.MethodGet, fmt.Sprintf("/api/v1/users/%d", env.admin.CID), nil, obsAccess)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("instructor1 cannot get other", func(t *testing.T) {
		w := env.doJSON(t, http.MethodGet, fmt.Sprintf("/api/v1/users/%d", env.observer.CID), nil, i1Access)
		assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	})

	t.Run("supervisor can get other", func(t *testing.T) {
		w := env.doJSON(t, http.MethodGet, fmt.Sprintf("/api/v1/users/%d", env.observer.CID), nil, supAccess)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		u := decodeUserData(t, decodeAPIV1(t, w))
		assert.Equal(t, env.observer.CID, u.CID)
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
		// Unique first name "Obs" → exactly one row; golden real pagination fields.
		w := env.doJSON(t, http.MethodGet,
			"/api/v1/users?q=Obs&page_size=10&sort=cid&dir=asc",
			nil, adminAccess)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		var envelope map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
		dataMap, ok := envelope["data"].(map[string]any)
		require.True(t, ok)
		items, ok := dataMap["items"].([]any)
		require.True(t, ok)
		require.Equal(t, 1, len(items), "q=Obs must match exactly one user: %s", w.Body.String())
		// Assert real pagination (do not rewrite total/pages/page/page_size).
		assert.Equal(t, float64(1), dataMap["total"])
		assert.Equal(t, float64(1), dataMap["pages"])
		assert.Equal(t, float64(1), dataMap["page"])
		assert.Equal(t, float64(10), dataMap["page_size"])
		item, ok := items[0].(map[string]any)
		require.True(t, ok)
		item["cid"] = float64(0)
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

// assertItemsNoPassword checks raw list envelope items lack a password key.
func assertItemsNoPassword(t *testing.T, body []byte) {
	t.Helper()
	var envelope map[string]any
	require.NoError(t, json.Unmarshal(body, &envelope))
	dataMap, ok := envelope["data"].(map[string]any)
	require.True(t, ok)
	items, ok := dataMap["items"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, items)
	item, ok := items[0].(map[string]any)
	require.True(t, ok)
	_, has := item["password"]
	assert.False(t, has, "list item must not include password key: %v", item)
}

// assertGetNoPassword checks raw get envelope data lacks a password key.
func assertGetNoPassword(t *testing.T, body []byte) {
	t.Helper()
	var envelope map[string]any
	require.NoError(t, json.Unmarshal(body, &envelope))
	dataMap, ok := envelope["data"].(map[string]any)
	require.True(t, ok)
	_, has := dataMap["password"]
	assert.False(t, has, "get data must not include password key: %v", dataMap)
}
