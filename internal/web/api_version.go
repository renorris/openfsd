package web

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

// Package-level API microversion registry (KD-3, KD-5, KD-19).
// Baseline is the UTC merge date of the versioning foundation PR.
const (
	apiMajorVersion = "v1"

	// First supported pin / current max (KD-19: UTC calendar date of PR-1 merge).
	apiMicroMin = "2026-07-28"
	apiMicroMax = "2026-07-28"

	headerAPIVersion          = "OpenFSD-API-Version"
	headerAPIMinVersion       = "OpenFSD-API-Min-Version"
	headerAPIMaxVersion       = "OpenFSD-API-Max-Version"
	headerAPIVersionDefaulted = "OpenFSD-API-Version-Defaulted" // "true" if header omitted

	apiVersionContextKey = "api_version"
)

// knownMicroversions lists every still-supported pin in ascending YYYY-MM-DD order.
var knownMicroversions = []string{
	"2026-07-28",
}

var (
	errAPIVersionInvalid = errors.New("invalid OpenFSD-API-Version")

	reAPIVersionDate  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	reAPIVersionAlias = regexp.MustCompile(`^1\.(\d{8})$`)
)

// apiVersionContext holds the resolved microversion for a request.
type apiVersionContext struct {
	Requested string // raw header (may be empty)
	Effective string // normalized pin used for shaping
	Defaulted bool   // true if header omitted (or empty after trim)
}

// normalizeAPIVersion converts a client version token into canonical YYYY-MM-DD.
// raw is the header value after the middleware has established it is non-empty.
// max is apiMicroMax (used for the "latest" alias).
// Membership in knownMicroversions is checked separately by the middleware.
func normalizeAPIVersion(raw, max string) (canonical string, err error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", errAPIVersionInvalid
	}

	// "latest" (any ASCII case) → max
	if strings.EqualFold(s, "latest") {
		if max == "" {
			return "", errAPIVersionInvalid
		}
		return max, nil
	}

	// Alias: 1.YYYYMMDD → YYYY-MM-DD
	if m := reAPIVersionAlias.FindStringSubmatch(s); m != nil {
		digits := m[1]
		s = digits[0:4] + "-" + digits[4:6] + "-" + digits[6:8]
	}

	if !reAPIVersionDate.MatchString(s) {
		return "", errAPIVersionInvalid
	}

	// Reject impossible calendar dates (e.g. 2026-02-30).
	if _, parseErr := time.ParseInLocation("2006-01-02", s, time.UTC); parseErr != nil {
		return "", errAPIVersionInvalid
	}

	return s, nil
}

func isKnownMicroversion(canonical string) bool {
	for _, v := range knownMicroversions {
		if v == canonical {
			return true
		}
	}
	return false
}

// shapeAdapter is one ordered microversion DTO shaper (KD-12).
// introducedAt is YYYY-MM-DD (or "epoch"); adapters are sorted ascending.
type shapeAdapter[T any, D any] struct {
	introducedAt string
	shape        func(T) D
}

// shapeFor selects the newest adapter with introducedAt <= effective (lexicographic ISO dates).
func shapeFor[T any, D any](effective string, adapters []shapeAdapter[T, D], domain T) D {
	if len(adapters) == 0 {
		var zero D
		return zero
	}
	for i := len(adapters) - 1; i >= 0; i-- {
		if adapters[i].introducedAt <= effective {
			return adapters[i].shape(domain)
		}
	}
	return adapters[0].shape(domain)
}

// useAPIV1Protected attaches dual-accept auth + CSRF + API microversion +
// Bearer actor revalidation middleware (KD-18).
func (s *Server) useAPIV1Protected(g *gin.RouterGroup) {
	g.Use(
		s.jwtBearerMiddleware,
		s.csrfIfCookieSession,
		s.apiVersionMiddleware,
		s.revalidateBearerActor, // DB overlay / reject inactive for Bearer (KD-18)
	)
}

// apiVersionMiddleware resolves OpenFSD-API-Version for dual-accept resource groups.
// Unknown/invalid pins → 400 envelope. Omitted header → max + Defaulted.
func (s *Server) apiVersionMiddleware(c *gin.Context) {
	raw := c.GetHeader(headerAPIVersion)
	trimmed := strings.TrimSpace(raw)

	ctx := apiVersionContext{Requested: raw}

	if trimmed == "" {
		ctx.Effective = apiMicroMax
		ctx.Defaulted = true
		c.Set(apiVersionContextKey, ctx)
		setAPIVersionHeaders(c)
		c.Next()
		return
	}

	canonical, err := normalizeAPIVersion(trimmed, apiMicroMax)
	if err != nil {
		setAPIVersionHeadersExplicit(c, apiMicroMax)
		slog.Warn("invalid OpenFSD-API-Version",
			"path", c.Request.URL.Path,
			"raw", trimmed,
			"cid", apiVersionLogCID(c),
		)
		res := newAPIV1Failure("invalid OpenFSD-API-Version")
		writeAPIV1Response(c, http.StatusBadRequest, &res)
		c.Abort()
		return
	}

	if !isKnownMicroversion(canonical) {
		setAPIVersionHeadersExplicit(c, apiMicroMax)
		slog.Warn("unsupported OpenFSD-API-Version",
			"path", c.Request.URL.Path,
			"raw", trimmed,
			"canonical", canonical,
			"cid", apiVersionLogCID(c),
		)
		msg := fmt.Sprintf("unsupported OpenFSD-API-Version %q; min=%s max=%s",
			canonical, apiMicroMin, apiMicroMax)
		res := newAPIV1Failure(msg)
		writeAPIV1Response(c, http.StatusBadRequest, &res)
		c.Abort()
		return
	}

	ctx.Effective = canonical
	ctx.Defaulted = false
	c.Set(apiVersionContextKey, ctx)
	setAPIVersionHeaders(c)
	c.Next()
}

// getAPIVersionContext returns the resolved microversion, if any.
func getAPIVersionContext(c *gin.Context) (apiVersionContext, bool) {
	val, ok := c.Get(apiVersionContextKey)
	if !ok {
		return apiVersionContext{}, false
	}
	ctx, ok := val.(apiVersionContext)
	return ctx, ok
}

// effectiveAPIVersion returns the effective pin, or apiMicroMax when unset.
func effectiveAPIVersion(c *gin.Context) string {
	if ctx, ok := getAPIVersionContext(c); ok && ctx.Effective != "" {
		return ctx.Effective
	}
	return apiMicroMax
}

// setAPIVersionHeaders writes version response headers from gin context.
// Safe no-op-with-soft-defaults when the version context is missing (public routes).
func setAPIVersionHeaders(c *gin.Context) {
	if ctx, ok := getAPIVersionContext(c); ok {
		setAPIVersionHeadersValues(c, ctx.Effective, ctx.Defaulted)
		return
	}
	// Soft observability headers on public / envelope responses without hard pin.
	setAPIVersionHeadersValues(c, apiMicroMax, false)
}

// setAPIVersionHeadersExplicit sets registry headers with a chosen effective pin
// (used when rejecting a bad pin before context is stored).
func setAPIVersionHeadersExplicit(c *gin.Context, effective string) {
	setAPIVersionHeadersValues(c, effective, false)
}

func setAPIVersionHeadersValues(c *gin.Context, effective string, defaulted bool) {
	if effective == "" {
		effective = apiMicroMax
	}
	h := c.Writer.Header()
	h.Set(headerAPIVersion, effective)
	h.Set(headerAPIMinVersion, apiMicroMin)
	h.Set(headerAPIMaxVersion, apiMicroMax)
	// Caches must vary on the client pin; merge if other middleware already set Vary.
	appendVary(h, headerAPIVersion)
	if defaulted {
		h.Set(headerAPIVersionDefaulted, "true")
	}
}

// appendVary adds value to the Vary header without duplicating an existing token.
func appendVary(h http.Header, value string) {
	existing := h.Get("Vary")
	if existing == "" {
		h.Set("Vary", value)
		return
	}
	for _, part := range strings.Split(existing, ",") {
		if strings.EqualFold(strings.TrimSpace(part), value) {
			return
		}
	}
	h.Set("Vary", existing+", "+value)
}

// apiVersionLogCID returns the authenticated CID when jwt middleware already ran, else 0.
// Never logs tokens.
func apiVersionLogCID(c *gin.Context) int {
	if claims := getJwtContext(c); claims != nil {
		return claims.CID
	}
	return 0
}

// discoveryData is the public version-discovery payload (KD-20).
type discoveryData struct {
	Major      string   `json:"major"`
	MinVersion string   `json:"min_version"`
	MaxVersion string   `json:"max_version"`
	Versions   []string `json:"versions"`
	Header     string   `json:"header"`
	Default    string   `json:"default"`
	OpenAPI    string   `json:"openapi"`
}

func newDiscoveryData() discoveryData {
	versions := make([]string, len(knownMicroversions))
	copy(versions, knownMicroversions)
	return discoveryData{
		Major:      apiMajorVersion,
		MinVersion: apiMicroMin,
		MaxVersion: apiMicroMax,
		Versions:   versions,
		Header:     headerAPIVersion,
		Default:    "max",
		OpenAPI:    "/api/v1/openapi.json",
	}
}

// handleAPIDiscovery serves GET /api/v1 and GET /api/v1/versions (public, version-agnostic).
func (s *Server) handleAPIDiscovery(c *gin.Context) {
	// Never reject on pin; still emit registry headers for observability.
	setAPIVersionHeadersValues(c, apiMicroMax, false)
	res := newAPIV1Success(newDiscoveryData())
	writeAPIV1Response(c, http.StatusOK, &res)
}

//go:embed openapi/openapi.v1.yaml
var openAPIYAML []byte

// handleOpenAPIYAML serves the embedded OpenAPI document as YAML (public, version-agnostic).
func (s *Server) handleOpenAPIYAML(c *gin.Context) {
	setAPIVersionHeadersValues(c, apiMicroMax, false)
	c.Header("Content-Type", "application/yaml; charset=utf-8")
	c.Writer.WriteHeader(http.StatusOK)
	_, _ = c.Writer.Write(openAPIYAML)
}

// handleOpenAPIJSON serves the embedded OpenAPI document converted to JSON.
func (s *Server) handleOpenAPIJSON(c *gin.Context) {
	setAPIVersionHeadersValues(c, apiMicroMax, false)
	body, err := openAPIYAMLToJSON(openAPIYAML)
	if err != nil {
		res := newAPIV1Failure("openapi conversion failed")
		writeAPIV1Response(c, http.StatusInternalServerError, &res)
		return
	}
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Writer.WriteHeader(http.StatusOK)
	_, _ = c.Writer.Write(body)
}

func openAPIYAMLToJSON(src []byte) ([]byte, error) {
	var doc any
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil, err
	}
	return json.Marshal(doc)
}
