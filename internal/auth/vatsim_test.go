package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVatsimAuth(t *testing.T) {
	s := AuthState{}

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

	s = AuthState{}
	// 48312 = TWRTrainer, 3ae3baf4 = initial challenge
	err = s.Initialize(48312, []byte("3ae3baf4"))
	assert.Nil(t, err)

	dst = s.GetResponseForChallenge([]byte("abcdef"))
	actual = string(dst[:])
	expected = "60ef113425658b09a1e555279d27f64a"
	assert.Equal(t, expected, actual)
}

func TestVatsimAuthUnsupportedClient(t *testing.T) {
	s := AuthState{}
	err := s.Initialize(1, []byte("challenge"))
	assert.ErrorIs(t, err, ErrUnsupportedAuthClient)
	assert.False(t, s.IsInitialized())
}

func TestVatsimAuthIsInitialized(t *testing.T) {
	s := AuthState{}
	assert.False(t, s.IsInitialized())

	err := s.Initialize(35044, []byte("30984979d8caed23"))
	assert.NoError(t, err)
	assert.True(t, s.IsInitialized())
}

func TestVatsimAuthAllKnownClients(t *testing.T) {
	// Exercise every known client id so obfuscation branches and key lookup stay covered.
	// clientId%3 selects the round permutation; clientId&1 swaps challenge halves.
	for clientId := range vatsimAuthKeys {
		s := AuthState{}
		err := s.Initialize(clientId, []byte("0123456789abcdef"))
		assert.NoError(t, err, "clientId %d", clientId)
		assert.True(t, s.IsInitialized())

		resp := s.GetResponseForChallenge([]byte("fedcba98"))
		assert.Len(t, resp[:], 32)
		s.UpdateState(&resp)

		resp2 := s.GetResponseForChallenge([]byte("aabbccdd"))
		assert.Len(t, resp2[:], 32)
		// After UpdateState the response for a new challenge must differ from first.
		assert.NotEqual(t, string(resp[:]), string(resp2[:]))
	}
}

func BenchmarkVatsimAuth(b *testing.B) {
	s := AuthState{}

	if err := s.Initialize(35044, []byte("0123456789abcdef")); err != nil {
		b.Fatal(err)
	}

	for i := 0; i < b.N; i++ {
		dst := s.GetResponseForChallenge([]byte("fedcba9876543210"))
		s.UpdateState(&dst)
	}
}
