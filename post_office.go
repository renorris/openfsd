package main

import (
	"errors"
	"github.com/mmcloughlin/geohash"
	"github.com/renorris/openfsd/protocol"
	"sync"
)

// PostOffice handles the routing of messages between clients
type PostOffice struct {
	clientRegistry     map[string]*FSDClient
	supervisorRegistry map[string]*FSDClient
	geohashRegistry    map[string][]*FSDClient
	lock               sync.RWMutex
}

const (
	MailTypeDirect = iota
	MailTypeBroadcastRanged
	MailTypeBroadcastAll
	MailTypeBroadcastSupervisors
)

// Mail holds messages to be passed between clients
type Mail struct {
	Type       int
	Source     *FSDClient
	Recipients []string
	Packets    []string
}

func NewMail(source *FSDClient) *Mail {
	return &Mail{
		Type:       0,
		Source:     source,
		Recipients: nil,
		Packets:    nil,
	}
}

func (m *Mail) SetType(mailType int) {
	m.Type = mailType
}

func (m *Mail) AddRecipient(callsign string) {
	if m.Recipients == nil {
		m.Recipients = make([]string, 0)
	}
	m.Recipients = append(m.Recipients, callsign)
}

func (m *Mail) AddPacket(packet string) {
	if m.Packets == nil {
		m.Packets = make([]string, 0)
	}
	m.Packets = append(m.Packets, packet)
}

type FSDClientNode struct {
	Client *FSDClient
	Next   *FSDClientNode
}

var (
	CallsignAlreadyRegisteredError = callsignAlreadyRegisteredError()
	CallsignNotRegisteredError     = callsignNotRegisteredError()
)

func callsignAlreadyRegisteredError() error { return errors.New("callsign already registered") }
func callsignNotRegisteredError() error     { return errors.New("callsign not registered") }

// RegisterCallsign registers a callsign to the post office, making it a valid recipient for other clients
func (p *PostOffice) RegisterCallsign(callsign string, client *FSDClient) error {
	p.lock.Lock()
	defer p.lock.Unlock()

	// Check if callsign already exists in the registry
	_, ok := p.clientRegistry[callsign]
	if ok {
		return CallsignAlreadyRegisteredError
	}

	// Otherwise, add the client to the registry
	p.clientRegistry[callsign] = client

	// If the client is a supervisor, add them to the supervisor registry
	if client.NetworkRating == protocol.NetworkRatingSUP {
		p.supervisorRegistry[callsign] = client
	}

	return nil
}

// DeregisterCallsign removes a callsign from the post office.
// Returns CallsignNotRegisteredError if the callsign is not registered.
func (p *PostOffice) DeregisterCallsign(callsign string) error {
	p.lock.Lock()
	defer p.lock.Unlock()

	// Check if callsign exists in registry
	client, ok := p.clientRegistry[callsign]
	if !ok {
		return CallsignNotRegisteredError
	}

	// Delete the entry
	delete(p.clientRegistry, callsign)

	// If the client is a supervisor, delete from supervisor registry
	if client.NetworkRating == protocol.NetworkRatingSUP {
		delete(p.supervisorRegistry, callsign)
	}

	// Remove client from geohash registry
	p.removeClientFromGeohashRegistry(client, client.CurrentGeohash)

	return nil
}

// SendMail forwards Mail to its recipients
func (p *PostOffice) SendMail(messages []Mail) {
	p.lock.RLock()
	defer p.lock.RUnlock()

	for _, msg := range messages {
		switch msg.Type {
		case MailTypeDirect:
			for _, recipient := range msg.Recipients {
				fsdClient, ok := p.clientRegistry[recipient]
				// If the callsign doesn't exist, drop the message
				if !ok {
					continue
				}
				for _, packet := range msg.Packets {
					// Do not block writing to mailbox, avoiding potential deadlock
					select {
					case fsdClient.Mailbox <- packet:
					default:
					}
				}
			}
		case MailTypeBroadcastRanged:
			neighbors := geohash.Neighbors(msg.Source.CurrentGeohash)
			searchHashes := append(neighbors, msg.Source.CurrentGeohash)
			for _, hash := range searchHashes {
				clients, ok := p.geohashRegistry[hash]
				if !ok {
					continue
				}
				for _, client := range clients {
					if msg.Source != client {
						for _, packet := range msg.Packets {
							select {
							case client.Mailbox <- packet:
							default:
							}
						}
					}
				}
			}
		case MailTypeBroadcastAll:
			for _, fsdClient := range p.clientRegistry {
				if msg.Source != fsdClient {
					for _, packet := range msg.Packets {
						select {
						case fsdClient.Mailbox <- packet:
						default:
						}
					}
				}
			}
		case MailTypeBroadcastSupervisors:
			for _, fsdClient := range p.supervisorRegistry {
				for _, packet := range msg.Packets {
					if msg.Source != fsdClient {
						select {
						case fsdClient.Mailbox <- packet:
						default:
						}
					}
				}
			}
		}

	}
}

func (p *PostOffice) removeClientFromGeohashRegistry(client *FSDClient, hash string) {
	clientList, ok := p.geohashRegistry[client.CurrentGeohash]
	if !ok {
		return
	}

	// Attempt to find the client in the list
	for i := 0; i < len(clientList); i++ {
		if clientList[i] == client {
			// Remove the client from the slice
			clientList = append(clientList[:i], clientList[i+1:]...)
			break
		}
	}

	// If that was the last client in the list, delete it from the map and return
	if len(clientList) == 0 {
		delete(p.geohashRegistry, client.CurrentGeohash)
		return
	}

	// Re-allocate the slice and rewrite the map entry
	newClientList := make([]*FSDClient, len(clientList))
	copy(newClientList, clientList)

	p.geohashRegistry[client.CurrentGeohash] = newClientList
}

// SetLocation updates the internal geohash tracking state for a client, if necessary
func (p *PostOffice) SetLocation(client *FSDClient, lat, lng float64) {
	// Check if the geohash has changed since we last updated
	hash := geohash.EncodeWithPrecision(lat, lng, 3)
	if hash == client.CurrentGeohash {
		return
	}

	p.lock.Lock()
	defer p.lock.Unlock()

	// Remove client from old geohash bucket
	p.removeClientFromGeohashRegistry(client, client.CurrentGeohash)

	// Find the new client list
	newClientList, ok := p.geohashRegistry[hash]
	// Create the slice if necessary
	if !ok {
		newClientList = make([]*FSDClient, 0)
	}

	// Add client to the list
	newClientList = append(newClientList, client)

	// Put it back on the registry
	p.geohashRegistry[hash] = newClientList

	// Set our new current geohash
	client.CurrentGeohash = hash
}

// GetClient finds an *FSDClient for a callsign string.
func (p *PostOffice) GetClient(callsign string) (*FSDClient, error) {
	p.lock.RLock()
	defer p.lock.RUnlock()

	client, ok := p.clientRegistry[callsign]
	if !ok {
		return nil, CallsignNotRegisteredError
	}

	return client, nil
}
