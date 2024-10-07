package postoffice

// Mail types
const (
	MailTypeDirect = iota
	MailTypeBroadcast
	MailTypeGeneralProximityBroadcast
	MailTypeCloseProximityBroadcast
	MailTypeSupervisorBroadcast
)

// Mail represents a message to be passed between addresses
type Mail struct {
	mailType  int
	source    Address
	recipient string
	packet    string
}

func (m *Mail) Type() int {
	return m.mailType
}

func (m *Mail) Source() Address {
	return m.source
}

func (m *Mail) Recipient() string {
	return m.recipient
}

func (m *Mail) Packet() string {
	return m.packet
}

func NewMail(source Address, mailType int, recipient string, packet string) Mail {
	return Mail{
		mailType:  mailType,
		source:    source,
		recipient: recipient,
		packet:    packet,
	}
}
