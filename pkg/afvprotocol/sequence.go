package afvprotocol

// ReceiveOutcome matches AFV-Native SequenceTest outcomes.
type ReceiveOutcome int

const (
	// ReceiveOK: in-window new sequence; accept and advance when possible.
	ReceiveOK ReceiveOutcome = iota
	// ReceiveBefore: duplicate or too old; drop.
	ReceiveBefore
	// ReceiveOverflow: beyond window; accept and jump window.
	ReceiveOverflow
)

// SequenceWindow is an AFV-Native SequenceTest port (sliding bitfield anti-replay).
// Window size is clamped to [1, 64] (uint64 bitfield width).
type SequenceWindow struct {
	bitfield uint64
	min      uint64
	window   uint
}

// NewSequenceWindow creates a window starting at startSequence (next expected).
// window is the sliding history size; 0 becomes 1; values >64 clamp to 64.
func NewSequenceWindow(startSequence uint64, window uint) *SequenceWindow {
	if window < 1 {
		window = 1
	}
	if window > SequenceWindowSize {
		window = SequenceWindowSize
	}
	return &SequenceWindow{min: startSequence, window: window}
}

// GetNext returns the next expected sequence number (_min).
func (s *SequenceWindow) GetNext() uint64 {
	if s == nil {
		return 0
	}
	return s.min
}

// Reset clears the window to start at sequence 0.
func (s *SequenceWindow) Reset() {
	if s == nil {
		return
	}
	s.min = 0
	s.bitfield = 0
}

// Received records newSequence and returns the AFV-Native outcome.
func (s *SequenceWindow) Received(newSequence uint64) ReceiveOutcome {
	if s == nil {
		return ReceiveBefore
	}
	if newSequence < s.min {
		return ReceiveBefore
	}
	if newSequence == s.min {
		s.advanceWindow()
		return ReceiveOK
	}
	// In-window future packet (bit index from min+1).
	if newSequence <= s.min+uint64(s.window) {
		bitidx := newSequence - s.min - 1
		mask := uint64(1) << bitidx
		if s.bitfield&mask == mask {
			return ReceiveBefore
		}
		s.bitfield |= mask
		return ReceiveOK
	}
	// Forced window jump (overflow).
	oldmin := s.min
	for s.min < oldmin+uint64(s.window) {
		s.advanceWindow()
		if s.min+uint64(s.window) > newSequence {
			break
		}
	}
	if s.min >= oldmin+uint64(s.window) {
		// Fully advanced: abandon old position and restart at newSequence+1.
		s.min = newSequence + 1
		s.bitfield = 0
		return ReceiveOverflow
	}
	bitidx := newSequence - s.min - 1
	mask := uint64(1) << bitidx
	s.bitfield |= mask
	return ReceiveOverflow
}

// advanceWindow shifts past the first contiguous received prefix.
// Port of AFV-Native SequenceTest::advanceWindow (ctz of inverted bitfield).
func (s *SequenceWindow) advanceWindow() {
	// bitflip and find first set bit (first unreceived packet in window).
	inv := ^s.bitfield
	idx := trailingZeros64(inv) + 1
	// If fully received (inv==0), ctz is 64 on uint64; +1 would be wrong.
	// AFV-Native: if !found then idx = 64 (sizeof*8), else idx++.
	// trailingZeros64(0) returns 64, so idx = 65; we clamp to window.
	if inv == 0 {
		idx = 64
	}
	if idx > uint(s.window) {
		idx = uint(s.window)
	}
	s.min += uint64(idx)
	if idx >= 64 {
		s.bitfield = 0
	} else {
		s.bitfield >>= idx
	}
}

// trailingZeros64 returns the number of trailing zero bits in x.
// For x==0 returns 64.
func trailingZeros64(x uint64) uint {
	if x == 0 {
		return 64
	}
	var n uint
	if x&0xffffffff == 0 {
		n += 32
		x >>= 32
	}
	if x&0xffff == 0 {
		n += 16
		x >>= 16
	}
	if x&0xff == 0 {
		n += 8
		x >>= 8
	}
	if x&0xf == 0 {
		n += 4
		x >>= 4
	}
	if x&0x3 == 0 {
		n += 2
		x >>= 2
	}
	if x&0x1 == 0 {
		n++
	}
	return n
}
