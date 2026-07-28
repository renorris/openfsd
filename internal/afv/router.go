package afv

import (
	"strings"

	"github.com/renorris/openfsd/pkg/afvprotocol"
)

// routeAT computes AR recipients for an inbound AudioTx from transmitter sess.
// Must be called with registry RLock held OR with a consistent snapshot of
// sess + index (we take RLock here).
//
// Returns per-recipient AR transceiver lists. Does not encrypt or send.
func (r *Registry) routeAT(tx *VoiceSession, at afvprotocol.AudioTx) []routeRecipient {
	if r == nil || tx == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Map TX transceiver ID → radio on the transmitter.
	txByID := make(map[uint16]Transceiver, len(tx.Transceivers))
	for _, t := range tx.Transceivers {
		txByID[t.ID] = t
	}

	// Accumulate RX radios per recipient session.
	type accKey struct {
		sess *VoiceSession
	}
	acc := make(map[*VoiceSession][]afvprotocol.RxTransceiver)

	edge := float64(0.1)
	if r.cfg != nil {
		edge = r.cfg.EdgeRatio()
	}

	for _, txID := range at.Transceivers {
		radio, ok := txByID[txID.ID]
		if !ok {
			continue
		}
		class := ClassifyRange(radio.Frequency, tx.IsATC)
		maxNM := float64(40)
		if r.cfg != nil {
			maxNM = r.cfg.MaxRangeNM(class)
		} else {
			maxNM = (&Config{}).MaxRangeNM(class)
		}
		maxM := NMToMeters(maxNM)
		cands := r.index.candidates(radio.Frequency, radio.LatDeg, radio.LonDeg, maxM)
		for _, c := range cands {
			if c.session == tx {
				continue // no hear-self
			}
			if c.session == nil || !c.session.Bound || c.session.UDPAddr == nil {
				continue
			}
			dist := SlantRangeM(radio.LatDeg, radio.LonDeg, c.lat, c.lon)
			ratio, ok := DistanceRatio(dist, maxM, edge)
			if !ok {
				continue
			}
			acc[c.session] = append(acc[c.session], afvprotocol.RxTransceiver{
				ID:            c.trxID,
				Frequency:     c.freqHz,
				DistanceRatio: ratio,
			})
		}
	}

	out := make([]routeRecipient, 0, len(acc))
	for sess, rxs := range acc {
		// de-dupe by trx ID
		seen := make(map[uint16]struct{}, len(rxs))
		uniq := make([]afvprotocol.RxTransceiver, 0, len(rxs))
		for _, rx := range rxs {
			if _, ok := seen[rx.ID]; ok {
				continue
			}
			seen[rx.ID] = struct{}{}
			uniq = append(uniq, rx)
		}
		out = append(out, routeRecipient{
			sess:  sess,
			udp:   sess.UDPAddr,
			rx:    uniq,
			rxKey: sess.ClientRxKey,
			tag:   sess.ChannelTag,
		})
	}
	return out
}

// callsignMatch reports case-insensitive callsign equality.
func callsignMatch(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}
