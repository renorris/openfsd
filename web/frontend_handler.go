package web

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"github.com/renorris/openfsd/auth"
	"github.com/renorris/openfsd/database"
	"github.com/renorris/openfsd/protocol"
	"github.com/renorris/openfsd/servercontext"
	"golang.org/x/crypto/bcrypt"
	"net/http"
	"slices"
	"strconv"
	"time"
)

//go:embed static
var StaticFS embed.FS

// FrontendHandler handles UI-related HTTP calls
func FrontendHandler(w http.ResponseWriter, r *http.Request) {

	r.Body = http.MaxBytesReader(w, r.Body, 1024)

	switch r.URL.Path {
	case "/login":
		loginHandler(w, r)
	case "/logout":
		logoutHandler(w, r)
	case "/dashboard":
		dashboardHandler(w, r)
	case "/admin_dashboard":
		adminDashboardHandler(w, r)
	case "/changepassword":
		changePasswordHandler(w, r)
	case "/":
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	default:
		http.NotFound(w, r)
	}
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		// Load login page
		if err := RenderTemplate(w, "login.html", nil); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	case "POST":
		// Handle login
		if err := r.ParseForm(); err != nil {
			http.Error(w, "unable to parse form values", http.StatusBadRequest)
		}

		var cid, password string
		if cid = r.PostForm.Get("cid"); cid == "" {
			http.Error(w, "CID query parameter not found", http.StatusBadRequest)
			return
		}
		if password = r.PostForm.Get("password"); password == "" {
			http.Error(w, "password query parameter not found", http.StatusBadRequest)
			return
		}

		var cidInt int
		var err error
		if cidInt, err = strconv.Atoi(cid); err != nil {
			http.Error(w, "invalid CID", http.StatusBadRequest)
			return
		}

		// Load user record from database
		userRecord := database.FSDUserRecord{}
		if err = userRecord.LoadByCID(servercontext.DB(), cidInt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "invalid login", http.StatusUnauthorized)
			} else {
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
			return
		}

		// Verify password
		if err = bcrypt.CompareHashAndPassword([]byte(userRecord.Password), []byte(password)); err != nil {
			http.Error(w, "invalid login", http.StatusUnauthorized)
			return
		}

		// Verify account standing
		if userRecord.NetworkRating <= protocol.NetworkRatingSUS {
			http.Error(w, "account suspended/inactive", http.StatusForbidden)
			return
		}

		// Administer a token
		// Use "dashboard" audience to specify that this is a web frontend token; not for connecting to FSD.
		claims := auth.NewFSDJWTClaims(
			userRecord.CID, userRecord.NetworkRating,
			userRecord.PilotRating, []string{"dashboard"})

		now := time.Now()
		expires := now.Add(24 * time.Hour)

		var token string
		if token, err = claims.MakeToken(expires); err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Set-Cookie", fmt.Sprintf("token=%s; Expires=%s", token, expires.Format(http.TimeFormat)))
		w.WriteHeader(http.StatusNoContent)
		return
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		deleteCookie(w, "token")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

func dashboardHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		var userRecord *database.FSDUserRecord
		var err error
		if userRecord, _, err = frontendSessionMiddleware(w, r); err != nil {
			return
		}

		if err = RenderTemplate(w, "dashboard.html", DashboardPageData{UserRecord: userRecord}); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

func changePasswordHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "POST":
		var userRecord *database.FSDUserRecord
		var err error
		if userRecord, _, err = frontendSessionMiddleware(w, r); err != nil {
			return
		}

		// Handle form parameters
		if err := r.ParseForm(); err != nil {
			http.Error(w, "unable to parse form values", http.StatusBadRequest)
		}

		var oldPassword, newPassword string
		var changeFSDPassword bool
		oldPassword = r.PostForm.Get("old_password")
		if newPassword = r.PostForm.Get("new_password"); newPassword == "" {
			http.Error(w, "new password query parameter not found", http.StatusBadRequest)
			return
		}
		if changeFSDPasswordStr := r.PostForm.Get("change_fsd_password"); changeFSDPasswordStr == "" {
			http.Error(w, "change fsd password query parameter not found", http.StatusBadRequest)
			return
		} else {
			switch changeFSDPasswordStr {
			case "true":
				changeFSDPassword = true
			case "false":
				changeFSDPassword = false
			default:
				http.Error(w, "change fsd password query parameter must be true or false", http.StatusBadRequest)
				return
			}
		}

		if len(newPassword) < 8 {
			http.Error(w, "password must be 8 or more characters", http.StatusBadRequest)
			return
		}

		if changeFSDPassword {
			userRecord.Password = ""
			userRecord.FSDPassword = newPassword
		} else {

			if err = bcrypt.CompareHashAndPassword([]byte(userRecord.Password), []byte(oldPassword)); err != nil {
				http.Error(w, "old password is incorrect", http.StatusUnauthorized)
				return
			}

			userRecord.FSDPassword = ""
			userRecord.Password = newPassword
		}

		if err = userRecord.Update(servercontext.DB()); err != nil {
			http.Error(w, "unable to update user record", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
		return
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

func adminDashboardHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":

		var claims *auth.FSDJWTClaims
		var err error
		if _, claims, err = frontendSessionMiddleware(w, r); err != nil {
			return
		}

		if claims.ControllerRating() < protocol.NetworkRatingSUP {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		if err = RenderTemplate(w, "admin_dashboard.html", nil); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

func deleteCookie(w http.ResponseWriter, name string) {
	w.Header().Set("Set-Cookie", fmt.Sprintf("%s=; Expires=%s", name, time.Unix(0, 0).Format(http.TimeFormat)))
}

func frontendSessionMiddleware(w http.ResponseWriter, r *http.Request) (userRecord *database.FSDUserRecord, claims *auth.FSDJWTClaims, err error) {
	// Get token cookie
	var tokenStr string
	if cookies := r.CookiesNamed("token"); len(cookies) != 1 {
		deleteCookie(w, "token")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		err = errors.New("invalid cookie length")
		return
	} else {
		tokenStr = cookies[0].Value
	}

	// Validate token
	var token *jwt.Token
	if token, err = (auth.DefaultVerifier{}).VerifyJWT(tokenStr); err != nil {
		deleteCookie(w, "token")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Parse claims
	claims = &auth.FSDJWTClaims{}
	if err = claims.Parse(token); err != nil {
		http.Error(w, "invalid token claims", http.StatusBadRequest)
		return
	}

	if !slices.Contains(claims.Audience(), "dashboard") {
		http.Error(w, "invalid token audience", http.StatusBadRequest)
		err = errors.New("token claims does not include 'dashboard'")
		return
	}

	// Load user record
	userRecord = &database.FSDUserRecord{}
	if err = userRecord.LoadByCID(servercontext.DB(), claims.CID()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "invalid CID", http.StatusForbidden)
		} else {
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}
		return
	}

	// Verify claims match database record
	if userRecord.CID != claims.CID() {
		http.Error(w, "claimed CID does not match CID on record", http.StatusForbidden)
		return
	}
	if userRecord.NetworkRating != claims.ControllerRating() {
		http.Error(w, "claimed network rating does not match rating on record", http.StatusForbidden)
		return
	}
	if userRecord.PilotRating != claims.PilotRating() {
		http.Error(w, "claimed pilot rating does not match rating on record", http.StatusForbidden)
		return
	}

	return userRecord, claims, nil
}
