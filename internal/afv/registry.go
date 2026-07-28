package afv

import (
	"crypto/rand"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/renorris/openfsd/pkg/afvprotocol"
)

var (
	errCallsignInUse   = errors.New("afv: callsign already in use")
	errSessionLimit    = errors.New("afv: session limit exceeded")
	errCIDSessionLimit = errors.New("afv: per-CID session limit exceeded")
	errNotFound        = errors.New("afv: session not found")
)

// Registry holds ephemeral voice sessions. Single RWMutex for P0.
// Never hold the lock across UDP WriteTo.
type Registry struct {
	mu         sync.RWMutex
	byTag      map[string]*VoiceSession
	byCallsign map[string]*VoiceSession         // upper callsign
	byCID      map[int]map[string]*VoiceSession // cid → callsignKey → sess
	index      *freqIndex
	cfg        *Config
}

func newRegistry(cfg *Config) *Registry {
	return &Registry{
		byTag:      make(map[string]*VoiceSession),
		byCallsign: make(map[string]*VoiceSession),
		byCID:      make(map[int]map[string]*VoiceSession),
		index:      newFreqIndex(),
		cfg:        cfg,
	}
}

// CreateOrReplace creates a new voice session. If callsign is held:
//   - strict mode → errCallsignInUse
//   - default → replace (tear down old, new keys/tag)
func (r *Registry) CreateOrReplace(cid int, callsign, clientName string, now time.Time) (*VoiceSession, error) {
	if r == nil {
		return nil, errNotFound
	}
	csKey := strings.ToUpper(strings.TrimSpace(callsign))
	if csKey == "" {
		return nil, errors.New("afv: empty callsign")
	}

	var rxKey, txKey [afvprotocol.KeySize]byte
	if _, err := rand.Read(rxKey[:]); err != nil {
		return nil, err
	}
	if _, err := rand.Read(txKey[:]); err != nil {
		return nil, err
	}
	tag := uuid.NewString()

	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.byCallsign[csKey]; ok {
		if r.cfg != nil && r.cfg.CallsignStrict {
			return nil, errCallsignInUse
		}
		r.removeLocked(existing)
	}

	// Global and per-CID limits (after possible replace free a slot).
	maxSess := 5000
	maxPerCID := 5
	if r.cfg != nil {
		if r.cfg.MaxSessions > 0 {
			maxSess = r.cfg.MaxSessions
		}
		if r.cfg.MaxSessionsPerCID > 0 {
			maxPerCID = r.cfg.MaxSessionsPerCID
		}
	}
	if len(r.byTag) >= maxSess {
		return nil, errSessionLimit
	}
	if len(r.byCID[cid]) >= maxPerCID {
		return nil, errCIDSessionLimit
	}

	sess := &VoiceSession{
		ChannelTag:  tag,
		CID:         cid,
		Callsign:    callsign,
		CallsignKey: csKey,
		ClientName:  clientName,
		ClientTxKey: txKey,
		ClientRxKey: rxKey,
		RxSeq:       afvprotocol.NewSequenceWindow(0, afvprotocol.SequenceWindowSize),
		Created:     now,
		IsATC:       IsATC(callsign, 0),
		CrossCouple: r.cfg == nil || r.cfg.CrossCouple,
	}
	r.byTag[tag] = sess
	r.byCallsign[csKey] = sess
	if r.byCID[cid] == nil {
		r.byCID[cid] = make(map[string]*VoiceSession)
	}
	r.byCID[cid][csKey] = sess
	return sess, nil
}

// Remove deletes a session by cid+callsign.
func (r *Registry) Remove(cid int, callsign string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	csKey := strings.ToUpper(strings.TrimSpace(callsign))
	sess := r.byCallsign[csKey]
	if sess == nil || sess.CID != cid {
		return errNotFound
	}
	r.removeLocked(sess)
	return nil
}

// UpdateTransceivers replaces the transceiver list and rebuilds index entries.
func (r *Registry) UpdateTransceivers(cid int, callsign string, trxs []Transceiver) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	csKey := strings.ToUpper(strings.TrimSpace(callsign))
	sess := r.byCallsign[csKey]
	if sess == nil || sess.CID != cid {
		return errNotFound
	}
	r.index.removeSession(sess)
	// copy slice
	cp := make([]Transceiver, len(trxs))
	copy(cp, trxs)
	sess.Transceivers = cp
	sess.IsATC = IsATC(sess.Callsign, len(cp))
	r.index.addSession(sess)
	return nil
}

// LookupByTag returns the session for a channel tag (RLock).
func (r *Registry) LookupByTag(tag string) *VoiceSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byTag[tag]
}

// BindUDP binds the first valid UDP source. Returns false if already bound to another addr.
func (r *Registry) BindUDP(sess *VoiceSession, addr netAddrStringer, now time.Time) (bound bool, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if sess == nil {
		return false, false
	}
	// re-check still registered
	if r.byTag[sess.ChannelTag] != sess {
		return false, false
	}
	if !sess.Bound {
		sess.UDPAddr = addr
		sess.Bound = true
		sess.touchUDP(now)
		return true, true
	}
	if sess.UDPAddr != nil && sess.UDPAddr.String() == addr.String() {
		sess.touchUDP(now)
		return true, true
	}
	// no rebind P0
	return false, false
}

// netAddrStringer is satisfied by net.Addr.
type netAddrStringer interface {
	Network() string
	String() string
}

// SnapshotForRoute copies route inputs under RLock for an AT.
// Returns transmitter session fields and candidate recipients outside the lock.
type routeRecipient struct {
	sess  *VoiceSession
	udp   netAddrStringer
	rx    []afvprotocol.RxTransceiver
	rxKey [afvprotocol.KeySize]byte
	tag   string
}

func (r *Registry) removeLocked(sess *VoiceSession) {
	if sess == nil {
		return
	}
	r.index.removeSession(sess)
	delete(r.byTag, sess.ChannelTag)
	delete(r.byCallsign, sess.CallsignKey)
	if m := r.byCID[sess.CID]; m != nil {
		delete(m, sess.CallsignKey)
		if len(m) == 0 {
			delete(r.byCID, sess.CID)
		}
	}
}

// Reap removes timed-out sessions. Returns number removed.
func (r *Registry) Reap(now time.Time) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	hbTO := 10 * time.Second
	idleTO := 30 * time.Second
	if r.cfg != nil {
		if r.cfg.HeartbeatTimeout > 0 {
			hbTO = r.cfg.HeartbeatTimeout
		}
		if r.cfg.SessionIdleTimeout > 0 {
			idleTO = r.cfg.SessionIdleTimeout
		}
	}
	var dead []*VoiceSession
	for _, s := range r.byTag {
		if s.Bound {
			last := s.lastUDPTime()
			if last.IsZero() || now.Sub(last) > hbTO {
				dead = append(dead, s)
			}
		} else {
			if now.Sub(s.Created) > idleTO {
				dead = append(dead, s)
			}
		}
	}
	for _, s := range dead {
		r.removeLocked(s)
	}
	return len(dead)
}

// Count returns session count.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byTag)
}
