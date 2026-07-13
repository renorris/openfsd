package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCookieSecureFlag(t *testing.T) {
	cases := []struct {
		name  string
		env   string
		tls   bool
		proto string
		want  bool
	}{
		{"default HTTP", "", false, "", false},
		{"default HTTPS TLS", "", true, "", true},
		{"default X-Forwarded-Proto https", "", false, "https", true},
		{"default X-Forwarded-Proto HTTP", "", false, "http", false},
		{"force true", "true", false, "", true},
		{"force TRUE", "TRUE", false, "", true},
		{"force 1", "1", false, "", true},
		{"force false", "false", true, "https", false},
		{"force 0", "0", true, "", false},
		{"force no", "no", true, "https", false},
		{"whitespace true", "  true  ", false, "", true},
		{"forwarded mixed case", "", false, "HTTPS", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cookieSecureFlag(tc.env, tc.tls, tc.proto)
			if got != tc.want {
				t.Fatalf("cookieSecureFlag(%q, tls=%v, proto=%q) = %v, want %v",
					tc.env, tc.tls, tc.proto, got, tc.want)
			}
		})
	}
}

func TestSetSessionCookieSecurePolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("COOKIE_SECURE=true forces Secure", func(t *testing.T) {
		ts := newTestServer(t)
		ts.cfg.CookieSecure = "true"

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "http://example.com/login", nil)

		user := createTestUser(t, ts, "pw", 1)
		if err := ts.setSessionCookie(c, user, false); err != nil {
			t.Fatal(err)
		}
		setCookie := w.Header().Get("Set-Cookie")
		if setCookie == "" {
			t.Fatal("expected Set-Cookie")
		}
		if !strings.Contains(strings.ToLower(setCookie), "secure") {
			t.Fatalf("expected Secure in Set-Cookie, got %q", setCookie)
		}
		if !strings.Contains(setCookie, "HttpOnly") {
			t.Fatalf("expected HttpOnly, got %q", setCookie)
		}
		if !strings.Contains(setCookie, "SameSite=Lax") {
			t.Fatalf("expected SameSite=Lax, got %q", setCookie)
		}
	})

	t.Run("default HTTP omits Secure", func(t *testing.T) {
		ts := newTestServer(t)
		ts.cfg.CookieSecure = ""

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "http://example.com/login", nil)

		user := createTestUser(t, ts, "pw", 1)
		if err := ts.setSessionCookie(c, user, false); err != nil {
			t.Fatal(err)
		}
		setCookie := w.Header().Get("Set-Cookie")
		// Cookie library may omit Secure entirely when false.
		if strings.Contains(strings.ToLower(setCookie), "secure") {
			t.Fatalf("did not expect Secure on HTTP default, got %q", setCookie)
		}
	})
}
