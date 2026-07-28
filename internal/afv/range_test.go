package afv

import "testing"

func TestIsATC(t *testing.T) {
	if !IsATC("BOS_TWR", 1) {
		t.Fatal("underscore")
	}
	if !IsATC("N123", 2) {
		t.Fatal("multi trx")
	}
	if IsATC("N123", 1) {
		t.Fatal("pilot")
	}
}

func TestDistanceRatio(t *testing.T) {
	r, ok := DistanceRatio(0, 1000, 0.1)
	if !ok || r != 1.0 {
		t.Fatalf("%v %v", r, ok)
	}
	r, ok = DistanceRatio(1000, 1000, 0.1)
	if !ok || r < 0.09 || r > 0.11 {
		t.Fatalf("edge %v", r)
	}
	_, ok = DistanceRatio(1001, 1000, 0.1)
	if ok {
		t.Fatal("out of range")
	}
}

func TestClassifyRange(t *testing.T) {
	if ClassifyRange(FrequencyUnicomHz, false) != RangeClassUnicom {
		t.Fatal()
	}
	if ClassifyRange(118700000, true) != RangeClassATC {
		t.Fatal()
	}
	if ClassifyRange(118700000, false) != RangeClassDefault {
		t.Fatal()
	}
}

func TestConfigMaxRange(t *testing.T) {
	var c *Config
	if c.MaxRangeNM(RangeClassUnicom) != 15 {
		t.Fatal()
	}
	cfg := &Config{RangeUnicomNM: 10, RangeDefaultNM: 20, RangeATCNM: 30}
	if cfg.MaxRangeNM(RangeClassUnicom) != 10 || cfg.MaxRangeNM(RangeClassDefault) != 20 || cfg.MaxRangeNM(RangeClassATC) != 30 {
		t.Fatal()
	}
}
