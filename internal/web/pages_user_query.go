package web

import (
	"net/url"
	"strconv"
	"strings"
)

// userDirectoryPageSize is the default directory page size (HTML omits page_size).
const userDirectoryPageSize = 50

// userDirectoryPageSizeMax matches db.UserListFilter hard cap.
const userDirectoryPageSizeMax = 200

// parseUserDirectoryQuery parses and clamps directory GET query parameters.
// Invalid values never 500: bad sort → cid, bad dir → asc, page < 1 → 1,
// rating outside [-1,12] or non-int → all (nil). Page clamping past the last
// page is applied later once Total is known (clampDirectoryPage).
//
// page_size (JSON API; HTML never sends it): missing/non-int/≤0 → 50;
// clamp to [1, 200] (UserListFilter hard cap).
func parseUserDirectoryQuery(values url.Values) userDirectoryQuery {
	q := userDirectoryQuery{
		Q:        strings.TrimSpace(values.Get("q")),
		Sort:     "cid",
		Desc:     false,
		Page:     1,
		PageSize: userDirectoryPageSize,
		Pages:    1,
	}

	switch strings.ToLower(strings.TrimSpace(values.Get("sort"))) {
	case "name":
		q.Sort = "name"
	case "rating":
		q.Sort = "rating"
	case "cid", "":
		q.Sort = "cid"
	default:
		q.Sort = "cid"
	}

	switch strings.ToLower(strings.TrimSpace(values.Get("dir"))) {
	case "desc":
		q.Desc = true
	default:
		q.Desc = false
	}

	if pageStr := strings.TrimSpace(values.Get("page")); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p >= 1 {
			q.Page = p
		}
	}

	// rating: missing / empty / invalid → nil (all). rating=0 is Suspended (valid).
	if ratingStr, ok := values["rating"]; ok && len(ratingStr) > 0 && strings.TrimSpace(ratingStr[0]) != "" {
		if r, err := strconv.Atoi(strings.TrimSpace(ratingStr[0])); err == nil && r >= -1 && r <= 12 {
			q.Rating = &r
		}
	}

	// page_size: optional (JSON directory API). HTML never sends it → stays 50.
	if psStr := strings.TrimSpace(values.Get("page_size")); psStr != "" {
		if ps, err := strconv.Atoi(psStr); err == nil && ps > 0 {
			if ps > userDirectoryPageSizeMax {
				ps = userDirectoryPageSizeMax
			}
			q.PageSize = ps
		}
		// non-int or ≤0 → keep default 50 (never 500)
	}

	return q
}

// clampDirectoryPage adjusts Page after Total is known so bookmarks past end still work.
func clampDirectoryPage(q *userDirectoryQuery) {
	if q.PageSize < 1 {
		q.PageSize = userDirectoryPageSize
	}
	pages := 1
	if q.Total > 0 {
		pages = (q.Total + q.PageSize - 1) / q.PageSize
	}
	if pages < 1 {
		pages = 1
	}
	q.Pages = pages
	if q.Page < 1 {
		q.Page = 1
	}
	if q.Page > q.Pages {
		q.Page = q.Pages
	}
}

// directoryValuesFromQuery encodes directory state for links/redirects.
// Always includes sort and dir for predictable filter-form round-trips.
// Omits empty q, nil rating, and page when page==1. Callers add cid/new/flash as needed.
func directoryValuesFromQuery(q userDirectoryQuery) url.Values {
	v := url.Values{}
	if q.Q != "" {
		v.Set("q", q.Q)
	}
	if q.Rating != nil {
		v.Set("rating", strconv.Itoa(*q.Rating))
	}
	sort := q.Sort
	if sort == "" {
		sort = "cid"
	}
	v.Set("sort", sort)
	if q.Desc {
		v.Set("dir", "desc")
	} else {
		v.Set("dir", "asc")
	}
	if q.Page > 1 {
		v.Set("page", strconv.Itoa(q.Page))
	}
	return v
}

// directoryValuesFromPost reads dir_* hidden fields from a create/update POST body.
func directoryValuesFromPost(post url.Values) url.Values {
	// Map dir_* → query keys then re-parse for clamp.
	mapped := url.Values{}
	if s := strings.TrimSpace(post.Get("dir_q")); s != "" {
		mapped.Set("q", s)
	}
	if s, ok := post["dir_rating"]; ok && len(s) > 0 {
		// Preserve empty string (all ratings) vs missing.
		mapped.Set("rating", strings.TrimSpace(s[0]))
	}
	if s := strings.TrimSpace(post.Get("dir_sort")); s != "" {
		mapped.Set("sort", s)
	}
	if s := strings.TrimSpace(post.Get("dir_dir")); s != "" {
		mapped.Set("dir", s)
	}
	if s := strings.TrimSpace(post.Get("dir_page")); s != "" {
		mapped.Set("page", s)
	}
	return directoryValuesFromQuery(parseUserDirectoryQuery(mapped))
}

// userEditorRedirect builds a PRG Location for /usereditor with directory params + cid + flash.
func userEditorRedirect(dir url.Values, cid int, flash string) string {
	v := url.Values{}
	for k, vals := range dir {
		for _, val := range vals {
			v.Add(k, val)
		}
	}
	if cid >= 1 {
		v.Set("cid", strconv.Itoa(cid))
	}
	if flash != "" {
		v.Set("flash", flash)
	}
	enc := v.Encode()
	if enc == "" {
		return "/usereditor"
	}
	return "/usereditor?" + enc
}

// directoryHref builds /usereditor?… from directory values plus optional extras (cid, new, page override).
func directoryHref(base url.Values, extras url.Values) string {
	v := url.Values{}
	for k, vals := range base {
		for _, val := range vals {
			v.Add(k, val)
		}
	}
	for k, vals := range extras {
		if len(vals) == 0 || vals[0] == "" {
			v.Del(k)
			continue
		}
		v.Set(k, vals[0])
	}
	enc := v.Encode()
	if enc == "" {
		return "/usereditor"
	}
	return "/usereditor?" + enc
}

// sortHref returns a sort-column link: toggle dir if active, else asc; page resets to 1; keeps cid if set.
func sortHref(q userDirectoryQuery, column string, selectedCID int) string {
	desc := false
	if q.Sort == column {
		desc = !q.Desc
	}
	nq := q
	nq.Sort = column
	nq.Desc = desc
	nq.Page = 1
	base := directoryValuesFromQuery(nq)
	extras := url.Values{}
	if selectedCID >= 1 {
		extras.Set("cid", strconv.Itoa(selectedCID))
	}
	return directoryHref(base, extras)
}

// pageHref returns a pager link for the given 1-based page, preserving selection.
func pageHref(q userDirectoryQuery, page, selectedCID int) string {
	nq := q
	nq.Page = page
	base := directoryValuesFromQuery(nq)
	extras := url.Values{}
	if selectedCID >= 1 {
		extras.Set("cid", strconv.Itoa(selectedCID))
	}
	return directoryHref(base, extras)
}

// filterRatingOptions builds −1…12 options with selection from Dir.Rating.
func filterRatingOptions(selected *int) []ratingOption {
	out := make([]ratingOption, 0, 14)
	for v := -1; v <= 12; v++ {
		sel := selected != nil && *selected == v
		label := networkRatingShort(v) + " — " + networkRatingLabel(v)
		out = append(out, ratingOption{Value: v, Label: label, Selected: sel})
	}
	return out
}

// dirRatingValue returns empty string if Rating is nil, else the number for hidden inputs.
func dirRatingValue(r *int) string {
	if r == nil {
		return ""
	}
	return strconv.Itoa(*r)
}

// RatingParam is the rating query value for templates (empty = all).
func (q userDirectoryQuery) RatingParam() string {
	return dirRatingValue(q.Rating)
}

// DirParam is "asc" or "desc" for templates.
func (q userDirectoryQuery) DirParam() string {
	if q.Desc {
		return "desc"
	}
	return "asc"
}
