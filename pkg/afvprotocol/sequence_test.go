package afvprotocol_test

import (
	"testing"

	"github.com/renorris/openfsd/pkg/afvprotocol"
)

// Ports AFV-Native test/cryptodto/test_SequenceTest.cpp outcomes.

func TestSequenceSimple1(t *testing.T) {
	s := afvprotocol.NewSequenceWindow(20, 8)

	if s.Received(20) != afvprotocol.ReceiveOK {
		t.Fatal("Didn't accept first sequence")
	}
	if s.GetNext() != 21 {
		t.Fatalf("didn't advance expected sequence: got %d", s.GetNext())
	}
	if s.Received(20) != afvprotocol.ReceiveBefore {
		t.Fatal("Didn't reject repeated sequence")
	}
	if s.Received(31) != afvprotocol.ReceiveOverflow {
		t.Fatal("Didn't overflow on +10 with window 8")
	}
}

func TestSequenceOutOfOrder1(t *testing.T) {
	s := afvprotocol.NewSequenceWindow(20, 8)
	if s.GetNext() != 20 {
		t.Fatalf("GetNext=%d", s.GetNext())
	}
	if s.Received(21) != afvprotocol.ReceiveOK {
		t.Fatal("Didn't accept 1 in future")
	}
	if s.GetNext() != 20 {
		t.Fatalf("Advanced sequence incorrectly: %d", s.GetNext())
	}
	if s.Received(20) != afvprotocol.ReceiveOK {
		t.Fatal("Didn't accept late next sequence")
	}
	if s.GetNext() != 22 {
		t.Fatalf("Didn't advance past received bits: %d", s.GetNext())
	}
}

func TestSequenceOutOfOrder2(t *testing.T) {
	s := afvprotocol.NewSequenceWindow(20, 8)
	if s.Received(21) != afvprotocol.ReceiveOK {
		t.Fatal("accept 21")
	}
	if s.GetNext() != 20 {
		t.Fatalf("next=%d", s.GetNext())
	}
	if s.Received(23) != afvprotocol.ReceiveOK {
		t.Fatal("accept 23")
	}
	if s.GetNext() != 20 {
		t.Fatalf("next=%d", s.GetNext())
	}
	if s.Received(20) != afvprotocol.ReceiveOK {
		t.Fatal("accept 20")
	}
	if s.GetNext() != 22 {
		t.Fatalf("next want 22 got %d", s.GetNext())
	}
	if s.Received(24) != afvprotocol.ReceiveOK {
		t.Fatal("accept 24")
	}
	if s.GetNext() != 22 {
		t.Fatalf("next want 22 got %d", s.GetNext())
	}
	if s.Received(22) != afvprotocol.ReceiveOK {
		t.Fatal("accept 22")
	}
	if s.GetNext() != 25 {
		t.Fatalf("next want 25 got %d", s.GetNext())
	}
}

func TestSequenceOutOfOrderReplay2(t *testing.T) {
	s := afvprotocol.NewSequenceWindow(20, 8)
	if s.Received(21) != afvprotocol.ReceiveOK {
		t.Fatal("21")
	}
	if s.Received(23) != afvprotocol.ReceiveOK {
		t.Fatal("23")
	}
	if s.Received(24) != afvprotocol.ReceiveOK {
		t.Fatal("24")
	}
	if s.GetNext() != 20 {
		t.Fatalf("next=%d", s.GetNext())
	}
	if s.Received(21) != afvprotocol.ReceiveBefore {
		t.Fatal("Accepted replay")
	}
}

func TestSequenceWindow64(t *testing.T) {
	s := afvprotocol.NewSequenceWindow(0, 64)
	if s.Received(0) != afvprotocol.ReceiveOK {
		t.Fatal("seq 0")
	}
	if s.GetNext() != 1 {
		t.Fatalf("next=%d", s.GetNext())
	}
	// Within window: min=1, window=64, max accepted without overflow = 1+64 = 65
	if s.Received(50) != afvprotocol.ReceiveOK {
		t.Fatal("50 in window")
	}
	// Beyond window from min=1: 1+64+1 = 66
	if s.Received(100) != afvprotocol.ReceiveOverflow {
		t.Fatal("want overflow for far future")
	}
}

func TestSequenceClampAndReset(t *testing.T) {
	s := afvprotocol.NewSequenceWindow(0, 0) // clamp to 1
	if s.Received(0) != afvprotocol.ReceiveOK {
		t.Fatal("0")
	}
	s2 := afvprotocol.NewSequenceWindow(0, 128) // clamp to 64
	_ = s2.Received(0)
	s2.Reset()
	if s2.GetNext() != 0 {
		t.Fatalf("after reset next=%d", s2.GetNext())
	}
}

func TestSequenceNilSafe(t *testing.T) {
	var s *afvprotocol.SequenceWindow
	if s.GetNext() != 0 {
		t.Fatal()
	}
	if s.Received(1) != afvprotocol.ReceiveBefore {
		t.Fatal()
	}
	s.Reset() // no panic
}
