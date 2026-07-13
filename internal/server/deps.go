package server

import (
	"context"
	"log/slog"
	"net"
	"time"

	"github.com/renorris/openfsd/db"
	"github.com/renorris/openfsd/internal/session"
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
type MetarQueue interface {
	Request(ctx context.Context, s session.Sender, icao string)
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
	// Listen optionally injects net listener creation (tests).
	// nil => net.ListenConfig{}.Listen
	Listen func(ctx context.Context, network, addr string) (net.Listener, error)
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func defaultListen(ctx context.Context, network, addr string) (net.Listener, error) {
	var lc net.ListenConfig
	return lc.Listen(ctx, network, addr)
}
