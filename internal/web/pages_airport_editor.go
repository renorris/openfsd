package web

import (
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

// Max body for airport-editor form payloads (mirrors sweatbox / FSD 2 MiB).
const airportEditorWebMaxBody = 2 << 20

// Max freeform flash message length embedded in redirect Location query.
const airportEditorFlashMsgMaxRunes = 240

// Safe download basename: no path separators, limited charset/length.
var airportEditorSafeFilenameRE = regexp.MustCompile(`^[A-Za-z0-9._\-]{1,64}$`)

// handleFrontendAirportEditor GET /airport-editor — server-rendered shell.
// No-JS path: dual textareas + CSRF echo-download forms. Map region is inert
// without JS (message only). No server-side parse or persistence.
func (s *Server) handleFrontendAirportEditor(c *gin.Context) {
	page := s.newAirportEditorPage(c)
	s.applyAirportEditorFlash(c, &page)
	s.writeTemplate(c, "airport_editor", page)
}

func (s *Server) newAirportEditorPage(c *gin.Context) airportEditorPage {
	claims := getJwtContext(c)
	return airportEditorPage{
		basePage: basePage{
			User:      pageUserFromClaims(claims),
			CSRFToken: s.issueCSRFToken(c),
		},
	}
}

func (s *Server) applyAirportEditorFlash(c *gin.Context, page *airportEditorPage) {
	msg := truncateRunes(strings.TrimSpace(c.Query("msg")), airportEditorFlashMsgMaxRunes)
	switch c.Query("flash") {
	case "ok":
		if msg == "" {
			msg = "Done"
		}
		page.FlashSuccess = msg
	case "err":
		if msg == "" {
			msg = "Request failed"
		}
		page.FlashError = msg
	}
}

// handleFrontendAirportEditorDownloadAPT POST /airport-editor/download-apt
// Echoes apt_text as a file attachment. Never writes to disk or DB.
func (s *Server) handleFrontendAirportEditorDownloadAPT(c *gin.Context) {
	s.handleAirportEditorDownload(c, "apt_text", "airport.apt", ".apt")
}

// handleFrontendAirportEditorDownloadAIR POST /airport-editor/download-air
// Echoes air_text as a file attachment. Never writes to disk or DB.
func (s *Server) handleFrontendAirportEditorDownloadAIR(c *gin.Context) {
	s.handleAirportEditorDownload(c, "air_text", "scenario.air", ".air")
}

// handleAirportEditorDownload is the shared pure-echo download path.
// bodyField is apt_text or air_text; defaultName/requiredExt define disposition.
func (s *Server) handleAirportEditorDownload(c *gin.Context, bodyField, defaultName, requiredExt string) {
	// Cap before form parse so oversized bodies fail closed early.
	// Parse form first (not validateCSRF-first) so a pure oversize wire body
	// surfaces as the size flash rather than a misleading 403: validateCSRF
	// would call PostForm, hit MaxBytesReader, and look like a CSRF miss.
	// Form contents are not trusted until CSRF succeeds below.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, airportEditorWebMaxBody+4096)
	if err := c.Request.ParseForm(); err != nil {
		if isRequestTooLarge(err) {
			s.redirectAirportEditorFlash(c, "err", "Payload too large (max 2 MiB)")
			return
		}
		s.redirectAirportEditorFlash(c, "err", "Invalid form")
		return
	}
	if !s.validateCSRF(c) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	text := c.PostForm(bodyField)
	if len(text) > airportEditorWebMaxBody {
		s.redirectAirportEditorFlash(c, "err", "Payload too large (max 2 MiB)")
		return
	}
	// Allow WIP text including incomplete content; reject only fully empty paste.
	if strings.TrimSpace(text) == "" {
		s.redirectAirportEditorFlash(c, "err", "Text is required to download")
		return
	}

	filename := safeAirportEditorFilename(c.PostForm("filename"), defaultName, requiredExt)

	// slog allowlist only — never log body text. Debug per design observability table.
	cid := 0
	if claims := getJwtContext(c); claims != nil {
		cid = claims.CID
	}
	slog.Debug("airport-editor echo-download",
		"cid", cid,
		"path", c.Request.URL.Path,
		"content_length", len(text),
		"filename", filename,
	)

	// Pure body echo — no parse, no os.WriteFile / Create / OpenFile.
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("Content-Disposition", contentDispositionAttachment(filename))
	c.Header("X-Content-Type-Options", "nosniff")
	c.String(http.StatusOK, text)
}

func (s *Server) redirectAirportEditorFlash(c *gin.Context, flash, msg string) {
	u := "/airport-editor?flash=" + url.QueryEscape(flash)
	if msg != "" {
		u += "&msg=" + url.QueryEscape(truncateRunes(msg, airportEditorFlashMsgMaxRunes))
	}
	c.Redirect(http.StatusSeeOther, u)
}

// safeAirportEditorFilename returns a basename matching the allowlist and
// required extension, or defaultName when the form value is unsafe.
func safeAirportEditorFilename(raw, defaultName, requiredExt string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultName
	}
	// Reject path components even if RE would miss some edge cases.
	base := path.Base(raw)
	if base != raw || base == "." || base == ".." {
		return defaultName
	}
	if !airportEditorSafeFilenameRE.MatchString(base) {
		return defaultName
	}
	if !strings.HasSuffix(strings.ToLower(base), strings.ToLower(requiredExt)) {
		return defaultName
	}
	return base
}

// contentDispositionAttachment builds a simple attachment disposition.
// filename is already restricted to [A-Za-z0-9._-], so quoting is safe.
func contentDispositionAttachment(filename string) string {
	return `attachment; filename="` + filename + `"`
}
