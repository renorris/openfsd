package db

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeRqlite is a minimal in-memory rqlite HTTP API for tests.
type fakeRqlite struct {
	mu     sync.Mutex
	users  map[int]map[string]any
	config map[string]string
	nextID int
	schema map[int]bool
}

func newFakeRqlite() *fakeRqlite {
	return &fakeRqlite{
		users:  make(map[int]map[string]any),
		config: make(map[string]string),
		nextID: 1,
		schema: make(map[int]bool),
	}
}

func (f *fakeRqlite) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/db/query", f.handleQuery)
	mux.HandleFunc("/db/execute", f.handleExecute)
	return mux
}

func (f *fakeRqlite) handleQuery(w http.ResponseWriter, r *http.Request) {
	var stmts [][]any
	_ = json.NewDecoder(r.Body).Decode(&stmts)
	f.mu.Lock()
	defer f.mu.Unlock()
	results := make([]rqliteResult, 0, len(stmts))
	for _, st := range stmts {
		sql := strings.ToLower(anyToString(st[0]))
		switch {
		case strings.Contains(sql, "select 1"):
			results = append(results, rqliteResult{Values: [][]any{{1}}})
		case strings.Contains(sql, "from users where cid"):
			cid := anyToInt(st[1])
			u, ok := f.users[cid]
			if !ok {
				results = append(results, rqliteResult{})
				continue
			}
			results = append(results, rqliteResult{
				Values: [][]any{{
					u["cid"], u["password"], u["first_name"], u["last_name"],
					u["network_rating"], u["pilot_rating"],
				}},
			})
		case strings.Contains(sql, "from config where key"):
			key := anyToString(st[1])
			v, ok := f.config[key]
			if !ok {
				results = append(results, rqliteResult{})
				continue
			}
			results = append(results, rqliteResult{Values: [][]any{{v}}})
		case strings.Contains(sql, "schema_migrations"):
			if strings.Contains(sql, "dirty = 1") {
				results = append(results, rqliteResult{})
				continue
			}
			var vals [][]any
			for ver := range f.schema {
				vals = append(vals, []any{ver})
			}
			results = append(results, rqliteResult{Values: vals})
		case strings.Contains(sql, "count(*)"):
			results = append(results, rqliteResult{Values: [][]any{{len(f.users)}}})
		case strings.Contains(sql, "from users"):
			var vals [][]any
			for _, u := range f.users {
				vals = append(vals, []any{
					u["cid"], u["first_name"], u["last_name"], u["network_rating"], u["pilot_rating"],
				})
			}
			results = append(results, rqliteResult{Values: vals})
		default:
			results = append(results, rqliteResult{})
		}
	}
	_ = json.NewEncoder(w).Encode(rqliteResponse{Results: results})
}

func (f *fakeRqlite) handleExecute(w http.ResponseWriter, r *http.Request) {
	var stmts [][]any
	_ = json.NewDecoder(r.Body).Decode(&stmts)
	f.mu.Lock()
	defer f.mu.Unlock()
	results := make([]rqliteResult, 0, len(stmts))
	for _, st := range stmts {
		sql := strings.ToLower(anyToString(st[0]))
		switch {
		case strings.Contains(sql, "create table"):
			results = append(results, rqliteResult{})
		case strings.Contains(sql, "insert into users"):
			id := f.nextID
			f.nextID++
			// args: hash, first, last, rating, pilot
			u := map[string]any{
				"cid": id, "password": anyToString(st[1]),
				"first_name": st[2], "last_name": st[3],
				"network_rating": anyToInt(st[4]), "pilot_rating": anyToInt(st[5]),
			}
			f.users[id] = u
			results = append(results, rqliteResult{LastInsertID: int64(id), RowsAffected: 1})
		case strings.Contains(sql, "insert into config"):
			key := anyToString(st[1])
			val := anyToString(st[2])
			if strings.Contains(sql, "do nothing") {
				if _, ok := f.config[key]; !ok {
					f.config[key] = val
				}
			} else {
				f.config[key] = val
			}
			results = append(results, rqliteResult{RowsAffected: 1})
		case strings.Contains(sql, "update users"):
			cid := anyToInt(st[len(st)-1])
			if _, ok := f.users[cid]; !ok {
				results = append(results, rqliteResult{RowsAffected: 0})
				continue
			}
			results = append(results, rqliteResult{RowsAffected: 1})
		case strings.Contains(sql, "delete from users"):
			cid := anyToInt(st[1])
			if _, ok := f.users[cid]; ok {
				delete(f.users, cid)
				results = append(results, rqliteResult{RowsAffected: 1})
			} else {
				results = append(results, rqliteResult{RowsAffected: 0})
			}
		case strings.Contains(sql, "schema_migrations"):
			if strings.Contains(sql, "insert") {
				ver := anyToInt(st[1])
				f.schema[ver] = true
			}
			results = append(results, rqliteResult{RowsAffected: 1})
		default:
			results = append(results, rqliteResult{})
		}
	}
	_ = json.NewEncoder(w).Encode(rqliteResponse{Results: results})
}

func TestRqliteUserAndConfig(t *testing.T) {
	fake := newFakeRqlite()
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	client := NewRqliteClient(srv.URL, RqliteClientOptions{})
	ctx := context.Background()
	if err := client.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	users := NewRqliteUserRepository(client, ReadWeak)
	cfg := NewRqliteConfigRepository(client, ReadWeak)

	u := &User{Password: "secret", NetworkRating: 1, PilotRating: 1}
	fn := "Test"
	u.FirstName = &fn
	if err := users.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if u.CID == 0 {
		t.Fatal("cid")
	}
	got, err := users.GetUserByCID(ctx, u.CID)
	if err != nil || got.NetworkRating != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	if !users.VerifyPasswordHash("secret", got.Password) {
		t.Fatal("password")
	}

	if err := cfg.Set(ctx, "K", "V"); err != nil {
		t.Fatal(err)
	}
	v, err := cfg.Get(ctx, "K")
	if err != nil || v != "V" {
		t.Fatalf("%q %v", v, err)
	}
	if err := cfg.SetIfNotExists(ctx, "K", "X"); err != nil {
		t.Fatal(err)
	}
	v, _ = cfg.Get(ctx, "K")
	if v != "V" {
		t.Fatalf("want V got %q", v)
	}
}

func TestMigrateRqlite(t *testing.T) {
	fake := newFakeRqlite()
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()
	client := NewRqliteClient(srv.URL, RqliteClientOptions{})
	if err := MigrateRqlite(context.Background(), client); err != nil {
		t.Fatal(err)
	}
	// second run no-op
	if err := MigrateRqlite(context.Background(), client); err != nil {
		t.Fatal(err)
	}
}

func TestParseReadLevel(t *testing.T) {
	if _, err := ParseReadLevel("nope"); err == nil {
		t.Fatal("want err")
	}
	l, err := ParseReadLevel("strong")
	if err != nil || l != ReadStrong {
		t.Fatal(l, err)
	}
}

func TestRequireDatabaseDriver(t *testing.T) {
	if err := RequireDatabaseDriver("sqlite"); err != nil {
		t.Fatal(err)
	}
	if err := RequireDatabaseDriver("rqlite"); err != nil {
		t.Fatal(err)
	}
	if err := RequireDatabaseDriver("postgres"); err == nil {
		t.Fatal("want err")
	}
}

func TestCaches(t *testing.T) {
	fake := newFakeRqlite()
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()
	client := NewRqliteClient(srv.URL, RqliteClientOptions{})
	ctx := context.Background()
	innerU := NewRqliteUserRepository(client, ReadWeak)
	innerC := NewRqliteConfigRepository(client, ReadWeak)
	uc := NewUserCache(innerU, 0)
	cc := NewConfigCache(innerC, 0)

	u := &User{Password: "pw", NetworkRating: 2}
	if err := uc.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	got, err := uc.GetUserByCID(ctx, u.CID)
	if err != nil || got.CID != u.CID {
		t.Fatal(err)
	}
	// second hit cache
	got2, _ := uc.GetUserByCID(ctx, u.CID)
	if got2.CID != u.CID {
		t.Fatal("cache miss?")
	}

	if err := cc.Set(ctx, ConfigJwtSecretKey, "abc"); err != nil {
		t.Fatal(err)
	}
	v, err := cc.Get(ctx, ConfigJwtSecretKey)
	if err != nil || v != "abc" {
		t.Fatal(v, err)
	}
}
