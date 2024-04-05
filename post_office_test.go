package main

import (
	"github.com/renorris/openfsd/protocol"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestPostOffice(t *testing.T) {
	configurePostOffice()

	c1 := FSDClient{
		Conn:          nil,
		Reader:        nil,
		AuthVerify:    nil,
		AuthSelf:      nil,
		NetworkRating: protocol.NetworkRatingOBS,
		Mailbox:       make(chan string, 16),
	}

	c2 := FSDClient{
		Conn:          nil,
		Reader:        nil,
		AuthVerify:    nil,
		AuthSelf:      nil,
		NetworkRating: protocol.NetworkRatingSUP,
		Mailbox:       make(chan string, 16),
	}

	// Test RegisterCallsign

	// Register "1"
	{
		err := PO.RegisterCallsign("1", &c1)
		assert.Nil(t, err)
	}

	// Register "1" twice
	{
		err := PO.RegisterCallsign("1", &c1)
		assert.NotNil(t, err)
		assert.ErrorIs(t, CallsignAlreadyRegisteredError, err)
	}

	// Register "2"
	{
		err := PO.RegisterCallsign("2", &c2)
		assert.Nil(t, err)
	}

	// Register "2" twice
	{
		err := PO.RegisterCallsign("2", &c2)
		assert.NotNil(t, err)
		assert.ErrorIs(t, CallsignAlreadyRegisteredError, err)
	}

	// Test SendMail
	{
		m := Mail{
			Type:       MailTypeDirect,
			Source:     &c1,
			Recipients: []string{"2"},
			Packets:    []string{"hello"},
		}

		PO.SendMail([]Mail{m})

		assert.Empty(t, c1.Mailbox)
		assert.NotEmpty(t, c2.Mailbox)

		msg := <-c2.Mailbox
		assert.Equal(t, "hello", msg)

		assert.Empty(t, c1.Mailbox)
		assert.Empty(t, c2.Mailbox)
	}

	{
		m := Mail{
			Type:       MailTypeBroadcastAll,
			Source:     &c1,
			Recipients: []string{"2"},
			Packets:    []string{"hello"},
		}

		PO.SendMail([]Mail{m})

		assert.Empty(t, c1.Mailbox)
		assert.NotEmpty(t, c2.Mailbox)

		{
			msg := <-c2.Mailbox
			assert.Equal(t, "hello", msg)
		}
	}

	// Test supervisor message
	{
		m := Mail{
			Type:       MailTypeBroadcastSupervisors,
			Source:     &c1,
			Recipients: nil,
			Packets:    []string{"hello"},
		}

		PO.SendMail([]Mail{m})
		assert.Empty(t, c1.Mailbox)
		assert.NotEmpty(t, c2.Mailbox)

		{
			msg := <-c2.Mailbox
			assert.Equal(t, "hello", msg)
		}
	}

	// Test ranged broadcast mail
	{
		// c1 and c2 in range:
		PO.SetLocation(&c1, 45.0, 45.0)
		PO.SetLocation(&c2, 45.0, 45.0)

		m := Mail{
			Type:       MailTypeBroadcastRanged,
			Source:     &c1,
			Recipients: nil,
			Packets:    []string{"hello"},
		}

		PO.SendMail([]Mail{m})

		// c2 should receive the broadcast
		assert.Empty(t, c1.Mailbox)
		assert.NotEmpty(t, c2.Mailbox)

		{
			msg := <-c2.Mailbox
			assert.Equal(t, "hello", msg)
		}

		// Move c2 far away from c1
		PO.SetLocation(&c2, -45.0, -45.0)

		// Try again, nobody should get anything now
		PO.SendMail([]Mail{m})

		assert.Empty(t, c1.Mailbox)
		assert.Empty(t, c2.Mailbox)

		// Move back
		PO.SetLocation(&c2, 45.0, 45.0)

		// Should get the message again
		PO.SendMail([]Mail{m})
		assert.Empty(t, c1.Mailbox)
		assert.NotEmpty(t, c2.Mailbox)

		{
			msg := <-c2.Mailbox
			assert.Equal(t, "hello", msg)
		}

		// Move c2 to a "neighbor" geohash cell
		PO.SetLocation(&c2, 45.7, 47.1)

		// c1 geohash: v00
		// c2 geohash: v01 (neighbor)

		PO.SendMail([]Mail{m})
		assert.Empty(t, c1.Mailbox)
		assert.NotEmpty(t, c2.Mailbox)

		{
			msg := <-c2.Mailbox
			assert.Equal(t, "hello", msg)
		}
	}

	// Test DeregisterCallsign

	// "3" is not registered
	{
		err := PO.DeregisterCallsign("3")
		assert.NotNil(t, err)
		assert.ErrorIs(t, CallsignNotRegisteredError, err)
	}

	// Unavailable client mailbox should not block SendMail
	{
		m := Mail{
			Type:       MailTypeDirect,
			Source:     &c1,
			Recipients: []string{"2"},
			Packets:    []string{"hello"},
		}

		c2.Mailbox = nil

		timer := time.NewTimer(100 * time.Millisecond)
		done := make(chan interface{})
		go func() {
			PO.SendMail([]Mail{m})
			close(done)
		}()

		select {
		case <-timer.C:
			assert.Fail(t, "SendMail timeout")
		case <-done:
		}
	}

	// Deregister "2"
	{
		err := PO.DeregisterCallsign("2")
		assert.Nil(t, err)
	}

	// Deregister "2" twice
	{
		err := PO.DeregisterCallsign("2")
		assert.NotNil(t, err)
		assert.ErrorIs(t, CallsignNotRegisteredError, err)
	}

	// Deregister "1"
	{
		err := PO.DeregisterCallsign("1")
		assert.Nil(t, err)
	}

	// Deregister "1" twice
	{
		err := PO.DeregisterCallsign("1")
		assert.NotNil(t, err)
		assert.ErrorIs(t, CallsignNotRegisteredError, err)
	}
}
