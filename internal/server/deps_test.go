package server

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/renorris/openfsd/internal/db"
	"github.com/renorris/openfsd/internal/postoffice"
	"github.com/renorris/openfsd/internal/session"
)

type stubUserStore struct{}

func (stubUserStore) GetUserByCID(ctx context.Context, cid int) (*db.User, error) {
	return nil, errors.New("not implemented")
}
func (stubUserStore) VerifyPasswordHash(plaintext, hash string) bool { return false }

type stubConfigStore struct{}

func (stubConfigStore) Get(ctx context.Context, key string) (string, error) {
	return "", errors.New("not found")
}

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
func (stubMetar) Run(ctx context.Context)                                    {}

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

func fullDeps() Deps {
	return Deps{
		Config:   &Config{FsdListenAddrs: []string{":0"}},
		Users:    stubUserStore{},
		ConfigKV: stubConfigStore{},
		Registry: stubRegistry{},
		Metar:    stubMetar{},
		Clock:    fixedClock{t: time.Unix(1_700_000_000, 0)},
	}
}

func TestNewRequiresDeps(t *testing.T) {
	_, err := New(Deps{})
	if err == nil {
		t.Fatal("expected error for empty Deps")
	}

	srv, err := New(fullDeps())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if srv.clock == nil {
		t.Fatal("clock should be set")
	}
	if srv.logger == nil {
		t.Fatal("logger should default")
	}
	if !srv.clock.Now().Equal(time.Unix(1_700_000_000, 0)) {
		t.Fatalf("clock not wired: %v", srv.clock.Now())
	}
}

func TestNewMissingRequiredFields(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Deps)
		wantSub string
	}{
		{"nil Config", func(d *Deps) { d.Config = nil }, "Config"},
		{"nil Users", func(d *Deps) { d.Users = nil }, "Users"},
		{"nil ConfigKV", func(d *Deps) { d.ConfigKV = nil }, "ConfigKV"},
		{"nil Registry", func(d *Deps) { d.Registry = nil }, "Registry"},
		{"nil Metar", func(d *Deps) { d.Metar = nil }, "Metar"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := fullDeps()
			tc.mutate(&d)
			_, err := New(d)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q should mention %q", err.Error(), tc.wantSub)
			}
		})
	}
}

func TestNewNilClockUsesReal(t *testing.T) {
	d := fullDeps()
	d.Clock = nil
	srv, err := New(d)
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

func TestRegistrySentinelsMatchPostoffice(t *testing.T) {
	// server re-exports must be identical for errors.Is across package boundaries.
	if !errors.Is(ErrCallsignInUse, postoffice.ErrCallsignInUse) {
		t.Fatal("ErrCallsignInUse mismatch")
	}
	if !errors.Is(ErrCallsignDoesNotExist, postoffice.ErrCallsignDoesNotExist) {
		t.Fatal("ErrCallsignDoesNotExist mismatch")
	}
	if !errors.Is(postoffice.ErrCallsignInUse, ErrCallsignInUse) {
		t.Fatal("reverse ErrCallsignInUse mismatch")
	}
}
