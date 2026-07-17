package server

import (
	"context"
	"log/slog"
	"net"
	"time"

	"github.com/renorris/openfsd/internal/db"
	"github.com/renorris/openfsd/internal/postoffice"
	"github.com/renorris/openfsd/internal/session"
)

// Registry sentinel errors — re-exported from postoffice so handlers/conn/HTTP
// can use errors.Is without importing the concrete registry package.
// Registry implementations should return these (or errors that wrap them).
var (
	ErrCallsignInUse        = postoffice.ErrCallsignInUse
	ErrCallsignDoesNotExist = postoffice.ErrCallsignDoesNotExist
)

// UserStore is the consumer-side user repository surface used by login.
// Signatures match db.UserRepository (no context yet).
type UserStore interface {
	GetUserByCID(cid int) (*db.User, error)
	VerifyPasswordHash(plaintext, hash string) bool
}

// ConfigStore is the consumer-side config KV surface.
// Signature matches db.ConfigRepository.Get (no context yet).
type ConfigStore interface {
	Get(key string) (string, error)
}

// Registry abstracts the callsign/geo registry (postoffice.PostOffice).
// Register/Find/Send should use ErrCallsignInUse / ErrCallsignDoesNotExist.
type Registry interface {
	Register(s *session.Session) error
	Release(s *session.Session)
	UpdatePosition(s *session.Session, center [2]float64, visRangeM float64)
	Search(s *session.Session, fn func(*session.Session) bool)
	All(except *session.Session, fn func(*session.Session) bool)
	Send(callsign, packet string) error
	Find(callsign string) (*session.Session, error)
	Snapshot() []*session.Session
}

// MetarQueue is the METAR fetch queue (metar.Service).
// Run starts workers; Request enqueues a fetch. Both are required so injectors
// cannot silently omit worker startup.
type MetarQueue interface {
	Request(ctx context.Context, s session.Sender, icao string)
	Run(ctx context.Context)
}

// Clock provides the current time (nil Deps.Clock => real wall clock).
type Clock interface {
	Now() time.Time
}

// Deps holds injectable dependencies for Server.
type Deps struct {
	Config   *Config
	Users    UserStore
	ConfigKV ConfigStore
	Registry Registry
	Metar    MetarQueue
	Clock    Clock // nil => real clock
	Logger   *slog.Logger
	// Listen optionally injects FSD net listener creation (tests).
	// nil => net.ListenConfig{}.Listen
	Listen func(ctx context.Context, network, addr string) (net.Listener, error)
	// HTTPListen optionally injects service HTTP listener creation (tests).
	// Signature matches net.Listen. nil => net.Listen("tcp", ServiceHTTPListenAddr).
	// Prefer returning a pre-bound listener so tests avoid bind/close/rebind TOCTOU.
	HTTPListen func(network, addr string) (net.Listener, error)
	// SweatboxEnabled gates SweatboxHost allocation. NewDefault copies from Config.
	// StartTestServer defaults true for e2e convenience.
	SweatboxEnabled bool
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func defaultListen(ctx context.Context, network, addr string) (net.Listener, error) {
	var lc net.ListenConfig
	return lc.Listen(ctx, network, addr)
}
