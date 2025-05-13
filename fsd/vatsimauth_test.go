package fsd

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestVatsimAuth(t *testing.T) {
	s := vatsimAuthState{}

	// 35044 = vPilot, 30984979d8caed23 = initial challenge
	err := s.Initialize(35044, []byte("30984979d8caed23"))
	assert.Nil(t, err)

	dst := s.GetResponseForChallenge([]byte("de6acb8e"))
	actual := string(dst[:])
	expected := "f8ee97157f66455ed6108fccef6ccf5f"
	assert.Equal(t, expected, actual)

	s.UpdateState(&dst)

	dst = s.GetResponseForChallenge([]byte("65b479573b0e"))
	actual = string(dst[:])
	expected = "8953f545c4e0ffd20943ad89b8ddd087"
	assert.Equal(t, expected, actual)

	s = vatsimAuthState{}
	// 48312 = TWRTrainer, 3ae3baf4 = initial challenge
	err = s.Initialize(48312, []byte("3ae3baf4"))
	assert.Nil(t, err)

	dst = s.GetResponseForChallenge([]byte("abcdef"))
	actual = string(dst[:])
	expected = "60ef113425658b09a1e555279d27f64a"
	assert.Equal(t, expected, actual)
}

func BenchmarkVatsimAuth(b *testing.B) {
	s := vatsimAuthState{}

	if err := s.Initialize(35044, []byte("0123456789abcdef")); err != nil {
		b.Fatal(err)
	}

	for i := 0; i < b.N; i++ {
		dst := s.GetResponseForChallenge([]byte("fedcba9876543210"))
		s.UpdateState(&dst)
	}
}
