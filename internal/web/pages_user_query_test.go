package web

import (
	"net/url"
	"testing"
)

func TestParseUserDirectoryQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		raw   string
		wantQ string
		wantR *int
		wantS string
		wantD bool
		wantP int
	}{
		{
			name:  "empty values",
			raw:   "",
			wantS: "cid",
			wantD: false,
			wantP: 1,
		},
		{
			name:  "rating missing",
			raw:   "q=smith",
			wantQ: "smith",
			wantS: "cid",
			wantP: 1,
		},
		{
			name:  "rating empty string",
			raw:   "rating=",
			wantS: "cid",
			wantP: 1,
		},
		{
			name:  "rating=0 suspended",
			raw:   "rating=0",
			wantR: intPtr(0),
			wantS: "cid",
			wantP: 1,
		},
		{
			name:  "rating=-1 inactive",
			raw:   "rating=-1",
			wantR: intPtr(-1),
			wantS: "cid",
			wantP: 1,
		},
		{
			name:  "rating=99 clamped to all",
			raw:   "rating=99",
			wantS: "cid",
			wantP: 1,
		},
		{
			name:  "rating=foo clamped to all",
			raw:   "rating=foo",
			wantS: "cid",
			wantP: 1,
		},
		{
			name:  "page=0 clamps to 1",
			raw:   "page=0",
			wantS: "cid",
			wantP: 1,
		},
		{
			name:  "page=-3 clamps to 1",
			raw:   "page=-3",
			wantS: "cid",
			wantP: 1,
		},
		{
			name:  "page=3 kept",
			raw:   "page=3",
			wantS: "cid",
			wantP: 3,
		},
		{
			name:  "sort=nope → cid",
			raw:   "sort=nope",
			wantS: "cid",
			wantP: 1,
		},
		{
			name:  "sort=name",
			raw:   "sort=name",
			wantS: "name",
			wantP: 1,
		},
		{
			name:  "sort=rating",
			raw:   "sort=rating",
			wantS: "rating",
			wantP: 1,
		},
		{
			name:  "dir=DESC",
			raw:   "dir=DESC",
			wantS: "cid",
			wantD: true,
			wantP: 1,
		},
		{
			name:  "dir=desc",
			raw:   "dir=desc",
			wantS: "cid",
			wantD: true,
			wantP: 1,
		},
		{
			name:  "dir=asc",
			raw:   "dir=asc",
			wantS: "cid",
			wantD: false,
			wantP: 1,
		},
		{
			name:  "combined",
			raw:   "q=Alice&rating=1&sort=name&dir=desc&page=2",
			wantQ: "Alice",
			wantR: intPtr(1),
			wantS: "name",
			wantD: true,
			wantP: 2,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vals, err := url.ParseQuery(tt.raw)
			if err != nil {
				t.Fatalf("ParseQuery: %v", err)
			}
			got := parseUserDirectoryQuery(vals)
			if got.Q != tt.wantQ {
				t.Errorf("Q=%q want %q", got.Q, tt.wantQ)
			}
			if !intPtrEq(got.Rating, tt.wantR) {
				t.Errorf("Rating=%v want %v", deref(got.Rating), deref(tt.wantR))
			}
			if got.Sort != tt.wantS {
				t.Errorf("Sort=%q want %q", got.Sort, tt.wantS)
			}
			if got.Desc != tt.wantD {
				t.Errorf("Desc=%v want %v", got.Desc, tt.wantD)
			}
			if got.Page != tt.wantP {
				t.Errorf("Page=%d want %d", got.Page, tt.wantP)
			}
			if got.PageSize != userDirectoryPageSize {
				t.Errorf("PageSize=%d want %d", got.PageSize, userDirectoryPageSize)
			}
		})
	}
}

func TestParseUserDirectoryQuery_PageSize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want int
	}{
		{name: "omitted", raw: "", want: 50},
		{name: "valid 25", raw: "page_size=25", want: 25},
		{name: "zero → default", raw: "page_size=0", want: 50},
		{name: "negative → default", raw: "page_size=-3", want: 50},
		{name: "non-int → default", raw: "page_size=nope", want: 50},
		{name: "cap 200", raw: "page_size=500", want: 200},
		{name: "boundary 200", raw: "page_size=200", want: 200},
		{name: "boundary 1", raw: "page_size=1", want: 1},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vals, err := url.ParseQuery(tt.raw)
			if err != nil {
				t.Fatalf("ParseQuery: %v", err)
			}
			got := parseUserDirectoryQuery(vals)
			if got.PageSize != tt.want {
				t.Errorf("PageSize=%d want %d", got.PageSize, tt.want)
			}
		})
	}
}

func TestDirectoryValuesFromPost(t *testing.T) {
	t.Parallel()
	post := url.Values{}
	post.Set("dir_q", " smith ")
	post.Set("dir_rating", "1")
	post.Set("dir_sort", "name")
	post.Set("dir_dir", "desc")
	post.Set("dir_page", "2")

	got := directoryValuesFromPost(post)
	if got.Get("q") != "smith" {
		t.Errorf("q=%q want smith", got.Get("q"))
	}
	if got.Get("rating") != "1" {
		t.Errorf("rating=%q want 1", got.Get("rating"))
	}
	if got.Get("sort") != "name" {
		t.Errorf("sort=%q want name", got.Get("sort"))
	}
	if got.Get("dir") != "desc" {
		t.Errorf("dir=%q want desc", got.Get("dir"))
	}
	if got.Get("page") != "2" {
		t.Errorf("page=%q want 2", got.Get("page"))
	}
}

func TestDirectoryValuesFromPostEmptyRating(t *testing.T) {
	t.Parallel()
	post := url.Values{}
	post.Set("dir_rating", "")
	post.Set("dir_sort", "cid")
	post.Set("dir_dir", "asc")
	got := directoryValuesFromPost(post)
	if got.Get("rating") != "" {
		// nil rating should omit rating key from directoryValuesFromQuery
		t.Errorf("rating should be omitted for all, got %q", got.Get("rating"))
	}
	if _, ok := got["rating"]; ok {
		t.Error("rating key should not be present for all-ratings")
	}
}

func TestUserEditorRedirect(t *testing.T) {
	t.Parallel()
	dir := url.Values{}
	dir.Set("q", "smith")
	dir.Set("sort", "name")
	dir.Set("dir", "asc")
	loc := userEditorRedirect(dir, 42, "created")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	if u.Path != "/usereditor" {
		t.Fatalf("path=%q", u.Path)
	}
	q := u.Query()
	if q.Get("cid") != "42" {
		t.Errorf("cid=%q", q.Get("cid"))
	}
	if q.Get("flash") != "created" {
		t.Errorf("flash=%q", q.Get("flash"))
	}
	if q.Get("q") != "smith" {
		t.Errorf("q=%q", q.Get("q"))
	}
}

func TestClampDirectoryPage(t *testing.T) {
	t.Parallel()
	q := userDirectoryQuery{Page: 99, PageSize: 50, Total: 10}
	clampDirectoryPage(&q)
	if q.Pages != 1 {
		t.Errorf("Pages=%d want 1", q.Pages)
	}
	if q.Page != 1 {
		t.Errorf("Page=%d want 1 (clamped)", q.Page)
	}

	q = userDirectoryQuery{Page: 2, PageSize: 2, Total: 5}
	clampDirectoryPage(&q)
	if q.Pages != 3 {
		t.Errorf("Pages=%d want 3", q.Pages)
	}
	if q.Page != 2 {
		t.Errorf("Page=%d want 2", q.Page)
	}
}

func TestUserDisplayName(t *testing.T) {
	t.Parallel()
	first := "Ada"
	last := "Lovelace"
	if got := userDisplayName(&first, &last, 7); got != "Ada Lovelace" {
		t.Errorf("got %q", got)
	}
	if got := userDisplayName(nil, nil, 7); got != "CID 7" {
		t.Errorf("got %q", got)
	}
	empty := ""
	if got := userDisplayName(&empty, &empty, 9); got != "CID 9" {
		t.Errorf("got %q", got)
	}
}

func TestNetworkRatingShort(t *testing.T) {
	t.Parallel()
	cases := map[int]string{
		-1: "INAC", 0: "SUSP", 1: "OBS", 11: "SUP", 12: "ADM",
	}
	for v, want := range cases {
		if got := networkRatingShort(v); got != want {
			t.Errorf("short(%d)=%q want %q", v, got, want)
		}
	}
}

func intPtr(v int) *int { return &v }

func intPtrEq(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func deref(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}
