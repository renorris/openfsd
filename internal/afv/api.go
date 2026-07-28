package afv

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/renorris/openfsd/internal/auth"
	"github.com/renorris/openfsd/pkg/afvprotocol"
	"github.com/renorris/openfsd/pkg/protocol"
)

// AuthRequest is POST /api/v1/auth body.
type AuthRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Client   string `json:"client"`
}

// PostCallsignResponse is the voice session create response.
type PostCallsignResponse struct {
	VoiceServer VoiceServerConnectionData `json:"voiceServer"`
}

// VoiceServerConnectionData holds UDP endpoints + channel keys.
type VoiceServerConnectionData struct {
	AddressIpV4   string                    `json:"addressIpV4"`
	AddressIpV6   string                    `json:"addressIpV6"`
	ChannelConfig afvprotocol.ChannelConfig `json:"channelConfig"`
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth", s.handleAuth)
	mux.HandleFunc("GET /api/v1/stations/aliased", s.withBearer(s.handleStationsAliased))
	mux.HandleFunc("POST /api/v1/users/{username}/callsigns/{callsign}", s.withBearer(s.handlePostCallsign))
	mux.HandleFunc("DELETE /api/v1/users/{username}/callsigns/{callsign}", s.withBearer(s.handleDeleteCallsign))
	mux.HandleFunc("POST /api/v1/users/{username}/callsigns/{callsign}/transceivers", s.withBearer(s.handlePostTransceivers))
	return mux
}

type bearerHandler func(w http.ResponseWriter, r *http.Request, claims *auth.CustomFields)

func (s *Server) withBearer(next bearerHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		const pfx = "Bearer "
		if !strings.HasPrefix(h, pfx) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		raw := strings.TrimSpace(h[len(pfx):])
		tok, err := auth.ParseJwtToken(raw, s.jwtSecret)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		cc := tok.CustomClaims()
		if cc == nil || cc.TokenType != "afv" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r, &cc.CustomFields)
	}
}

func (s *Server) handleAuth(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var req AuthRequest
	if err := json.Unmarshal(body, &req); err != nil || strings.TrimSpace(req.Username) == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	cid, err := strconv.Atoi(strings.TrimSpace(req.Username))
	if err != nil || cid <= 0 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	ip := clientIP(r)
	now := time.Now()
	if !s.authFail.Allow(ip, now) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}

	user, err := s.users.GetUserByCID(r.Context(), cid)
	hash := dummyBcryptHash
	var rating protocol.NetworkRating = protocol.NetworkRatingObserver
	if err == nil && user != nil {
		hash = user.Password
		rating = protocol.NetworkRating(user.NetworkRating)
	}
	ok := s.users.VerifyPasswordHash(req.Password, hash)
	if !ok {
		s.authFail.Record(ip, now)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if user != nil && user.NetworkRating == int(protocol.NetworkRatingSuspended) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	tok, err := auth.MakeJwtToken(&auth.CustomFields{
		TokenType:     "afv",
		CID:           cid,
		NetworkRating: rating,
	}, s.cfg.JWTTTL)
	if err != nil {
		slog.Error("AFV MakeJwtToken", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	raw, err := tok.SignedString(s.jwtSecret)
	if err != nil {
		slog.Error("AFV SignedString", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(raw))
	if req.Client != "" {
		slog.Debug("AFV auth ok", "cid", cid, "client", req.Client)
	}
}

func (s *Server) handleStationsAliased(w http.ResponseWriter, r *http.Request, _ *auth.CustomFields) {
	// P0: always empty array (route must not 404).
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("[]"))
}

func (s *Server) handlePostCallsign(w http.ResponseWriter, r *http.Request, claims *auth.CustomFields) {
	username := r.PathValue("username")
	callsign := r.PathValue("callsign")
	if strconv.Itoa(claims.CID) != username {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if strings.TrimSpace(callsign) == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if s.cfg.UDPAdvertiseIPv4 == "" {
		http.Error(w, "server misconfigured: AFV_UDP_ADVERTISE_IPV4", http.StatusServiceUnavailable)
		return
	}

	sess, replaced, err := s.reg.CreateOrReplace(claims.CID, callsign, "", time.Now())
	if err != nil {
		if errors.Is(err, errCallsignInUse) {
			http.Error(w, "callsign in use", http.StatusConflict)
			return
		}
		if errors.Is(err, errSessionLimit) || errors.Is(err, errCIDSessionLimit) {
			http.Error(w, "session limit", http.StatusTooManyRequests)
			return
		}
		slog.Error("AFV create session", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Mesh leave for replaced session after unlock (no Delta until trx POST).
	if replaced != "" {
		s.meshPublishLeaves([]string{replaced})
	}

	resp := PostCallsignResponse{
		VoiceServer: VoiceServerConnectionData{
			AddressIpV4: s.cfg.UDPAdvertiseIPv4,
			AddressIpV6: s.cfg.UDPAdvertiseIPv6,
			ChannelConfig: afvprotocol.ChannelConfig{
				ChannelTag:      sess.ChannelTag,
				AeadReceiveKey:  sess.ClientRxKey[:],
				AeadTransmitKey: sess.ClientTxKey[:],
				HmacKey:         nil,
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleDeleteCallsign(w http.ResponseWriter, r *http.Request, claims *auth.CustomFields) {
	username := r.PathValue("username")
	callsign := r.PathValue("callsign")
	if strconv.Itoa(claims.CID) != username {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if left, err := s.reg.Remove(claims.CID, callsign); err == nil && left != "" {
		s.meshPublishLeaves([]string{left})
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handlePostTransceivers(w http.ResponseWriter, r *http.Request, claims *auth.CustomFields) {
	username := r.PathValue("username")
	callsign := r.PathValue("callsign")
	if strconv.Itoa(claims.CID) != username {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var trxs []Transceiver
	if err := json.Unmarshal(body, &trxs); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	isATC, trxsCopy, err := s.reg.UpdateTransceivers(claims.CID, callsign, trxs)
	if err != nil {
		if errors.Is(err, errNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.meshPublishDelta(callsign, isATC, trxsCopy)
	w.WriteHeader(http.StatusOK)
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
