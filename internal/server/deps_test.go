package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/renorris/openfsd/db"
	"github.com/renorris/openfsd/internal/session"
)

type stubUserStore struct{}

func (stubUserStore) GetUserByCID(cid int) (*db.User, error) {
	return nil, errors.New("not implemented")
}
func (stubUserStore) VerifyPasswordHash(plaintext, hash string) bool { return false }

type stubConfigStore struct{}

func (stubConfigStore) Get(key string) (string, error) { return "", errors.New("not found") }

type stubRegistry struct{}

func (stubRegistry) Register(s *session.Session) error { return nil }
func (stubRegistry) Release(s *session.Session)        {}
func (stubRegistry) UpdatePosition(s *session.Session, center [2]float64, visRangeM float64) {
}
func (stubRegistry) Search(s *session.Session, fn func(*session.Session) bool) {}
func (stubRegistry) All(except *session.Session, fn func(*session.Session) bool) {
}
func (stubRegistry) Send(callsign, packet string) error             { return nil }
func (stubRegistry) Find(callsign string) (*session.Session, error) { return nil, errors.New("no") }
func (stubRegistry) Snapshot() []*session.Session                   { return nil }

type stubMetar struct{}

func (stubMetar) Request(ctx context.Context, s session.Sender, icao string) {}

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

func TestNewRequiresDeps(t *testing.T) {
	_, err := New(Deps{})
	if err == nil {
		t.Fatal("expected error for empty Deps")
	}

	cfg := &Config{FsdListenAddrs: []string{":0"}}
	srv, err := New(Deps{
		Config:   cfg,
		Users:    stubUserStore{},
		ConfigKV: stubConfigStore{},
		Registry: stubRegistry{},
		Metar:    stubMetar{},
		Clock:    fixedClock{t: time.Unix(1_700_000_000, 0)},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if srv.clock == nil {
		t.Fatal("clock should be set")
	}
	if srv.logger == nil {
		t.Fatal("logger should default")
	}
	if srv.listen == nil {
		t.Fatal("listen should default")
	}
	if !srv.clock.Now().Equal(time.Unix(1_700_000_000, 0)) {
		t.Fatalf("clock not wired: %v", srv.clock.Now())
	}
}

func TestNewNilClockUsesReal(t *testing.T) {
	srv, err := New(Deps{
		Config:   &Config{},
		Users:    stubUserStore{},
		ConfigKV: stubConfigStore{},
		Registry: stubRegistry{},
		Metar:    stubMetar{},
	})
	if err != nil {
		t.Fatal(err)
	}
	// realClock.Now should be near wall clock
	delta := time.Since(srv.clock.Now())
	if delta < 0 {
		delta = -delta
	}
	if delta > time.Second {
		t.Fatalf("real clock skew too large: %v", delta)
	}
}
