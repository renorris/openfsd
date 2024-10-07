package client

import (
	"errors"
	"fmt"
	"github.com/renorris/openfsd/handler"
	"github.com/renorris/openfsd/protocol"
	"github.com/renorris/openfsd/servercontext"
	"log"
)

// EventLoop runs the main event loop for a logged in client
func (c *FSDClient) EventLoop() error {

	infoStr := fmt.Sprintf("callsign=%s cid=%d rating=%s name=\"%s\" ip=%s", c.callsign, c.cid, c.networkRating.String(), c.realName, c.RemoteNetworkAddrString())
	log.Printf("client_connected total_clients=%d %s", servercontext.PostOffice().NumRegistered(), infoStr)
	defer func() {
		log.Printf("client_disconnected total_clients=%d %s", servercontext.PostOffice().NumRegistered()-1, infoStr)
	}()

	// Post-login FSD client event loop
	for {
		select {

		// Close on context elapse
		case <-c.connection.ctx.Done():
			return nil

		// Handle incoming packets
		case packet := <-c.connection.readerChan:
			if shouldDisconnect, err := c.handleIncomingPacket(packet); err != nil {
				return err
			} else if shouldDisconnect {
				return nil
			}

		// Handle incoming mail
		case mailPacket := <-c.mailbox:
			if err := c.connection.WritePacket(mailPacket); err != nil {
				return err
			}

		// Handle incoming kill signals
		case killPacket := <-c.kill:
			return c.connection.WritePacketImmediately(killPacket)
		}
	}
}

func (c *FSDClient) handleIncomingPacket(packet string) (shouldDisconnect bool, err error) {
	// Find a handler for this packet
	var h handler.Handler
	if h, err = handler.New(packet); err != nil {
		// Check if the error is an FSD error.
		// If so, gracefully send it to the client. If not, return.
		var fsdError *protocol.FSDError
		if errors.As(err, &fsdError) {
			if err = c.connection.WritePacket(fsdError.Serialize()); err != nil {
				return
			}
		}
		return
	}

	// Run the handler function
	var result handler.Result
	if result, err = h(c, packet); err != nil {
		// Check if the error is an FSD error.
		// If so, gracefully send it to the client. If not, return.
		var fsdError *protocol.FSDError
		if errors.As(err, &fsdError) {
			if err = c.connection.WritePacket(fsdError.Serialize()); err != nil {
				return
			}
		} else {
			return
		}
	}

	// Send replies
	if replies := result.Replies(); replies != nil {
		for _, r := range replies {
			if err = c.connection.WritePacket(r); err != nil {
				return
			}
		}
	}

	// Send mail
	if mailingList := result.MailingList(); mailingList != nil {
		for _, mail := range mailingList {
			servercontext.PostOffice().SendMail(&mail)
		}
	}

	// disconnect if flagged
	shouldDisconnect = result.DisconnectFlag()

	return
}
