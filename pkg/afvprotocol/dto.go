package afvprotocol

import (
	"fmt"
)

// Heartbeat is client→server H DTO: msgpack [Callsign].
type Heartbeat struct {
	Callsign string
}

// EncodeMsgpack encodes H as a 1-element array.
func (h Heartbeat) EncodeMsgpack() []byte {
	dst := encodeArrayHeader(nil, 1)
	return encodeString(dst, h.Callsign)
}

// DecodeHeartbeat parses H msgpack.
func DecodeHeartbeat(b []byte) (Heartbeat, error) {
	d := decoder{b: b}
	n, err := d.arrayLen()
	if err != nil {
		return Heartbeat{}, err
	}
	if n != 1 {
		return Heartbeat{}, fmt.Errorf("%w: H expected array len 1, got %d", errMsgpackType, n)
	}
	cs, err := d.string()
	if err != nil {
		return Heartbeat{}, err
	}
	return Heartbeat{Callsign: cs}, nil
}

// TxTransceiver is a TX radio reference in AT: msgpack [ID u16].
type TxTransceiver struct {
	ID uint16
}

// RxTransceiver is an RX radio entry in AR: msgpack [ID u16, Frequency u32, DistanceRatio f32].
type RxTransceiver struct {
	ID            uint16
	Frequency     uint32 // Hz
	DistanceRatio float32
}

// AudioTx is client→server AT:
// msgpack [Callsign, SequenceCounter u32, Audio bin, LastPacket bool, Transceivers [][ID]].
type AudioTx struct {
	Callsign        string
	SequenceCounter uint32
	Audio           []byte
	LastPacket      bool
	Transceivers    []TxTransceiver
}

// EncodeMsgpack encodes AT.
func (a AudioTx) EncodeMsgpack() []byte {
	dst := encodeArrayHeader(nil, 5)
	dst = encodeString(dst, a.Callsign)
	dst = encodeUint64(dst, uint64(a.SequenceCounter))
	dst = encodeBin(dst, a.Audio)
	dst = encodeBool(dst, a.LastPacket)
	dst = encodeArrayHeader(dst, len(a.Transceivers))
	for _, t := range a.Transceivers {
		dst = encodeArrayHeader(dst, 1)
		dst = encodeUint64(dst, uint64(t.ID))
	}
	return dst
}

// DecodeAudioTx parses AT msgpack.
func DecodeAudioTx(b []byte) (AudioTx, error) {
	d := decoder{b: b}
	n, err := d.arrayLen()
	if err != nil {
		return AudioTx{}, err
	}
	if n != 5 {
		return AudioTx{}, fmt.Errorf("%w: AT expected array len 5, got %d", errMsgpackType, n)
	}
	cs, err := d.string()
	if err != nil {
		return AudioTx{}, err
	}
	seq, err := d.uint32()
	if err != nil {
		return AudioTx{}, err
	}
	audio, err := d.bin()
	if err != nil {
		return AudioTx{}, err
	}
	last, err := d.bool()
	if err != nil {
		return AudioTx{}, err
	}
	tn, err := d.arrayLen()
	if err != nil {
		return AudioTx{}, err
	}
	trxs := make([]TxTransceiver, 0, tn)
	for i := 0; i < tn; i++ {
		an, err := d.arrayLen()
		if err != nil {
			return AudioTx{}, err
		}
		if an != 1 {
			return AudioTx{}, fmt.Errorf("%w: TxTransceiver expected array len 1, got %d", errMsgpackType, an)
		}
		id, err := d.uint16()
		if err != nil {
			return AudioTx{}, err
		}
		trxs = append(trxs, TxTransceiver{ID: id})
	}
	return AudioTx{
		Callsign:        cs,
		SequenceCounter: seq,
		Audio:           audio,
		LastPacket:      last,
		Transceivers:    trxs,
	}, nil
}

// AudioRx is server→client AR:
// msgpack [Callsign, SequenceCounter u32, Audio bin, LastPacket bool, Transceivers [][ID,Freq,Ratio]].
type AudioRx struct {
	Callsign        string
	SequenceCounter uint32
	Audio           []byte
	LastPacket      bool
	Transceivers    []RxTransceiver
}

// EncodeMsgpack encodes AR.
func (a AudioRx) EncodeMsgpack() []byte {
	dst := encodeArrayHeader(nil, 5)
	dst = encodeString(dst, a.Callsign)
	dst = encodeUint64(dst, uint64(a.SequenceCounter))
	dst = encodeBin(dst, a.Audio)
	dst = encodeBool(dst, a.LastPacket)
	dst = encodeArrayHeader(dst, len(a.Transceivers))
	for _, t := range a.Transceivers {
		dst = encodeArrayHeader(dst, 3)
		dst = encodeUint64(dst, uint64(t.ID))
		dst = encodeUint64(dst, uint64(t.Frequency))
		dst = encodeFloat32(dst, t.DistanceRatio)
	}
	return dst
}

// DecodeAudioRx parses AR msgpack.
func DecodeAudioRx(b []byte) (AudioRx, error) {
	d := decoder{b: b}
	n, err := d.arrayLen()
	if err != nil {
		return AudioRx{}, err
	}
	if n != 5 {
		return AudioRx{}, fmt.Errorf("%w: AR expected array len 5, got %d", errMsgpackType, n)
	}
	cs, err := d.string()
	if err != nil {
		return AudioRx{}, err
	}
	seq, err := d.uint32()
	if err != nil {
		return AudioRx{}, err
	}
	audio, err := d.bin()
	if err != nil {
		return AudioRx{}, err
	}
	last, err := d.bool()
	if err != nil {
		return AudioRx{}, err
	}
	tn, err := d.arrayLen()
	if err != nil {
		return AudioRx{}, err
	}
	trxs := make([]RxTransceiver, 0, tn)
	for i := 0; i < tn; i++ {
		an, err := d.arrayLen()
		if err != nil {
			return AudioRx{}, err
		}
		if an != 3 {
			return AudioRx{}, fmt.Errorf("%w: RxTransceiver expected array len 3, got %d", errMsgpackType, an)
		}
		id, err := d.uint16()
		if err != nil {
			return AudioRx{}, err
		}
		freq, err := d.uint32()
		if err != nil {
			return AudioRx{}, err
		}
		ratio, err := d.float32()
		if err != nil {
			return AudioRx{}, err
		}
		trxs = append(trxs, RxTransceiver{ID: id, Frequency: freq, DistanceRatio: ratio})
	}
	return AudioRx{
		Callsign:        cs,
		SequenceCounter: seq,
		Audio:           audio,
		LastPacket:      last,
		Transceivers:    trxs,
	}, nil
}
