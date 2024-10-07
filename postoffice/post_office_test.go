package postoffice

import (
	"errors"
	"github.com/renorris/openfsd/protocol"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

type testAddress struct {
	name    string
	mailbox chan string
	killbox chan string
	rating  protocol.NetworkRating
	geohash Geohash
}

func (t *testAddress) Name() string {
	return t.name
}

func (t *testAddress) SendMail(s string) {
	select {
	case t.mailbox <- s:
	default:
	}
}

func (t *testAddress) SendKill(s string) error {
	select {
	case t.killbox <- s:
		return nil
	default:
		return errors.New("killbox defaulted")
	}
}

func (t *testAddress) NetworkRating() protocol.NetworkRating {
	return t.rating
}

func (t *testAddress) Geohash() Geohash {
	return t.geohash
}

func (t *testAddress) State() AddressState {
	return AddressState{}
}

func TestPostOffice(t *testing.T) {
	PO := NewPostOffice()

	c1 := testAddress{
		name:    "1",
		mailbox: make(chan string, 16),
		killbox: make(chan string, 16),
		rating:  protocol.NetworkRatingOBS,
		geohash: Geohash{},
	}

	c2 := testAddress{
		name:    "2",
		mailbox: make(chan string, 16),
		killbox: make(chan string, 16),
		rating:  protocol.NetworkRatingSUP,
		geohash: Geohash{},
	}

	// Test RegisterAddress

	// Register "1"
	{
		err := PO.RegisterAddress(&c1)
		assert.Nil(t, err)
	}

	// Register "1" twice
	{
		err := PO.RegisterAddress(&c1)
		assert.NotNil(t, err)
		assert.ErrorIs(t, KeyInUseError, err)
	}

	// Register "2"
	{
		err := PO.RegisterAddress(&c2)
		assert.Nil(t, err)
	}

	// Register "2" twice
	{
		err := PO.RegisterAddress(&c2)
		assert.NotNil(t, err)
		assert.ErrorIs(t, KeyInUseError, err)
	}

	// Test SendMail
	{
		m := Mail{
			mailType:  MailTypeDirect,
			source:    &c1,
			recipient: "2",
			packet:    "hello",
		}

		PO.SendMail(&m)

		assert.Empty(t, c1.mailbox)
		assert.NotEmpty(t, c2.mailbox)

		msg := <-c2.mailbox
		assert.Equal(t, "hello", msg)

		assert.Empty(t, c1.mailbox)
		assert.Empty(t, c2.mailbox)
	}

	{
		m := Mail{
			mailType:  MailTypeBroadcast,
			source:    &c1,
			recipient: "2",
			packet:    "hello",
		}

		PO.SendMail(&m)

		assert.Empty(t, c1.mailbox)
		assert.NotEmpty(t, c2.mailbox)

		{
			msg := <-c2.mailbox
			assert.Equal(t, "hello", msg)
		}
	}

	// Test supervisor message
	{
		m := Mail{
			mailType: MailTypeSupervisorBroadcast,
			source:   &c1,
			packet:   "hello",
		}

		PO.SendMail(&m)
		assert.Empty(t, c1.mailbox)
		assert.NotEmpty(t, c2.mailbox)

		{
			msg := <-c2.mailbox
			assert.Equal(t, "hello", msg)
		}
	}

	// Test ranged broadcast mail
	{
		// c1 and c2 in range:
		c1.geohash = PO.SetLocation(&c1, 45.0, 45.0)
		c2.geohash = PO.SetLocation(&c2, 45.0, 45.0)

		m := Mail{
			mailType: MailTypeGeneralProximityBroadcast,
			source:   &c1,
			packet:   "hello",
		}

		PO.SendMail(&m)

		// c2 should receive the broadcast
		assert.Empty(t, c1.mailbox)
		assert.NotEmpty(t, c2.mailbox)

		{
			msg := <-c2.mailbox
			assert.Equal(t, "hello", msg)
		}

		// Move c2 far away from c1
		c2.geohash = PO.SetLocation(&c2, -45.0, -45.0)

		// Try again, nobody should see anything now
		PO.SendMail(&m)
		assert.Empty(t, c1.mailbox)
		assert.Empty(t, c2.mailbox)

		// Move back
		c2.geohash = PO.SetLocation(&c2, 45.0, 45.0)

		// Should see the message again
		PO.SendMail(&m)
		assert.Empty(t, c1.mailbox)
		assert.NotEmpty(t, c2.mailbox)

		{
			msg := <-c2.mailbox
			assert.Equal(t, "hello", msg)
		}

		// Move c2 to a general proximity neighbor cell
		c2.geohash = PO.SetLocation(&c2, 45.7, 47.1)

		// c1 geohash: v00
		// c2 geohash: v01 (neighbor)

		PO.SendMail(&m)
		assert.Empty(t, c1.mailbox)
		assert.NotEmpty(t, c2.mailbox)

		{
			msg := <-c2.mailbox
			assert.Equal(t, "hello", msg)
		}

		// Test close proximity broadcast
		m2 := Mail{
			mailType: MailTypeCloseProximityBroadcast,
			source:   &c1,
			packet:   "hello",
		}

		// Set client 1 and client 2 in the same location
		c1.geohash = PO.SetLocation(&c1, 32.713026, -117.176283)
		c2.geohash = PO.SetLocation(&c2, 32.713026, -117.176283)

		PO.SendMail(&m2)
		assert.Empty(t, c1.mailbox)
		assert.NotEmpty(t, c2.mailbox)

		{
			msg := <-c2.mailbox
			assert.Equal(t, "hello", msg)
		}

		// Move client 2 to a neighbor close-proximity cell
		c2.geohash = PO.SetLocation(&c2, 32.719491, -117.134442)

		// client 2 should still see close-proximity mail
		PO.SendMail(&m2)
		assert.Empty(t, c1.mailbox)
		assert.NotEmpty(t, c2.mailbox)

		{
			msg := <-c2.mailbox
			assert.Equal(t, "hello", msg)
		}

		// Move c2 two close-proximity cells away
		c2.geohash = PO.SetLocation(&c2, 32.715297, -117.091257)

		// Nobody should see any mail
		PO.SendMail(&m2)
		assert.Empty(t, c1.mailbox)
		assert.Empty(t, c2.mailbox)
	}

	// Unavailable browser mailbox should not block SendMail
	{
		m := Mail{
			mailType:  MailTypeDirect,
			source:    &c1,
			recipient: "2",
			packet:    "hello",
		}

		c2.mailbox = nil

		timer := time.NewTimer(100 * time.Millisecond)
		done := make(chan interface{})
		go func() {
			PO.SendMail(&m)
			close(done)
		}()

		select {
		case <-timer.C:
			assert.Fail(t, "SendMail timeout")
		case <-done:
		}
	}

	// Deregister "2"
	PO.DeregisterAddress(&c2)

	// Deregister "2" twice
	PO.DeregisterAddress(&c2)

	// Deregister "1"
	PO.DeregisterAddress(&c1)

	// Deregister "1" twice
	PO.DeregisterAddress(&c1)
}
