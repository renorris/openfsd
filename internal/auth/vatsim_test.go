package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVatsimAuth(t *testing.T) {
	s := AuthState{}

	// 35044 = vPilot (even, %3==1), 30984979d8caed23 = initial challenge
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
	// 48312 = TWRTrainer (even, %3==0), 3ae3baf4 = initial challenge
	err = s.Initialize(48312, []byte("3ae3baf4"))
	assert.Nil(t, err)

	dst = s.GetResponseForChallenge([]byte("abcdef"))
	actual = string(dst[:])
	expected = "60ef113425658b09a1e555279d27f64a"
	assert.Equal(t, expected, actual)
}

// Wire-locked goldens covering branches the historical vectors miss:
// odd clientId (challenge half-swap) and clientId%3==2 permutation.
func TestVatsimAuthOddClientAndMod3BranchGoldens(t *testing.T) {
	// 27095 = Euroscope: odd (clientId&1), %3==2
	s := AuthState{}
	err := s.Initialize(27095, []byte("0123456789abcdef"))
	assert.NoError(t, err)

	dst := s.GetResponseForChallenge([]byte("de6acb8e"))
	assert.Equal(t, "6c7d657f338683d2f77883631dceb9eb", string(dst[:]))

	s.UpdateState(&dst)
	dst = s.GetResponseForChallenge([]byte("65b479573b0e"))
	assert.Equal(t, "b5931c1b1dffedfb0f088779cec8b927", string(dst[:]))

	// 24515 = vatSys: odd, %3==2 (different key material)
	s = AuthState{}
	err = s.Initialize(24515, []byte("3ae3baf4abcd1234"))
	assert.NoError(t, err)
	dst = s.GetResponseForChallenge([]byte("abcdef12"))
	assert.Equal(t, "1abfc1856275a6f4445749df59e1400a", string(dst[:]))

	// 8464 = vSTARS: even, %3==1 (locks a non-vPilot %3==1 key)
	s = AuthState{}
	err = s.Initialize(8464, []byte("30984979d8caed23"))
	assert.NoError(t, err)
	dst = s.GetResponseForChallenge([]byte("de6acb8e"))
	assert.Equal(t, "d1cc03852d9a8364d3fb76234e70d96a", string(dst[:]))
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
