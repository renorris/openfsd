package handler

import "github.com/renorris/openfsd/postoffice"

// Result represents the result of a handler function
type Result struct {
	replies        []string
	mail           []postoffice.Mail
	disconnectFlag bool
}

// Replies returns the slice of reply packets.
// Return value may be nil.
func (r *Result) Replies() []string {
	return r.replies
}

// MailingList returns the slice of mail.
// Return value may be nil.
func (r *Result) MailingList() []postoffice.Mail {
	return r.mail
}

// DisconnectFlag returns whether the flag is set indicating to disconnect the client
func (r *Result) DisconnectFlag() bool {
	return r.disconnectFlag
}

// setDisconnectFlag sets the disconnect flag to true, which will signal the event loop to disconnect the client
func (r *Result) setDisconnectFlag() {
	r.disconnectFlag = true
}

// addReply adds a packet to be sent back to the caller client
func (r *Result) addReply(packet string) {
	if r.replies == nil {
		r.replies = make([]string, 0, 1)
	}
	r.replies = append(r.replies, packet)
}

// AddMail adds mail to be sent to other clients
func (r *Result) addMail(mail postoffice.Mail) {
	if r.mail == nil {
		r.mail = make([]postoffice.Mail, 0, 1)
	}
	r.mail = append(r.mail, mail)
}
