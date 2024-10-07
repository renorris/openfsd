package service

import (
	"context"
	"encoding/json"
	"github.com/renorris/openfsd/postoffice"
	"github.com/renorris/openfsd/protocol"
	"github.com/renorris/openfsd/servercontext"
	"log"
	"time"
)

// DataFeedService polls the server post office every 15 seconds and
// generates a JSON string representing the server state. Conventionally,
// this file is obtained via the HTTP endpoint at /data/openfsd-data.json
type DataFeedService struct{}

type general struct {
	Version          int       `json:"version"`           // Major version of the data feed
	UpdateTimestamp  time.Time `json:"update_timestamp"`  // The last time the data feed was updated
	ConnectedClients int       `json:"connected_clients"` // Number of clients connected
	UniqueUsers      int       `json:"unique_users"`      // Number of unique users connected
}

type pilot struct {
	Callsign    string               `json:"callsign"`
	CID         int                  `json:"cid"`
	Name        string               `json:"name"`
	PilotRating protocol.PilotRating `json:"pilot_rating"`
	Latitude    float64              `json:"latitude"`
	Longitude   float64              `json:"longitude"`
	Altitude    int                  `json:"altitude"`
	Groundspeed int                  `json:"groundspeed"`
	Transponder string               `json:"transponder"`
	Heading     int                  `json:"heading"`      // Degrees magnetic
	LastUpdated time.Time            `json:"last_updated"` // The time this pilot's information was last updated
}

type controllerRating struct {
	ID        protocol.NetworkRating `json:"id"`         // Controller NetworkRating ID
	ShortName string                 `json:"short_name"` // Short identifier
	LongName  string                 `json:"long_name"`  // Human-readable long name
}

type pilotRating struct {
	ID        protocol.PilotRating `json:"id"`         // pilot NetworkRating ID
	ShortName string               `json:"short_name"` // Short identifier
	LongName  string               `json:"long_name"`  // Human-readable long name
}

type schema struct {
	General      general            `json:"general"`
	Pilots       []pilot            `json:"pilots"`
	Ratings      []controllerRating `json:"ratings"`
	PilotRatings []pilotRating      `json:"pilot_ratings"`
}

func (s *DataFeedService) Start(ctx context.Context, doneErr chan<- error) (err error) {

	readySig := make(chan struct{})

	// boot data feed service on its own goroutine
	go func() {
		doneErr <- s.boot(ctx, readySig)
	}()

	// Wait for the ready signal
	<-readySig
	log.Println("Data feed service running.")

	return nil
}

func (s *DataFeedService) boot(ctx context.Context, readySig chan struct{}) error {

	// Run a tick first
	if err := s.tick(); err != nil {
		close(readySig)
		return err
	}

	close(readySig)

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	// Loop until context close. On tick, update the feed.
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := s.tick(); err != nil {
				return err
			}
		}
	}
}

func (s *DataFeedService) tick() error {
	// Load all pilot data from post office
	numRegistered := servercontext.PostOffice().NumRegistered()
	pilots := make([]pilot, 0, numRegistered+32)

	uniqueCIDs := make(map[int]*interface{}, numRegistered+32)

	servercontext.PostOffice().ForEachRegistered(func(name string, address postoffice.Address) bool {
		state := address.State()

		// Build pilot data
		p := pilot{
			Callsign:    name,
			CID:         state.CID,
			Name:        state.RealName,
			PilotRating: state.PilotRating,
			Latitude:    state.Latitude,
			Longitude:   state.Longitude,
			Altitude:    state.Altitude,
			Groundspeed: state.Groundspeed,
			Transponder: state.Transponder,
			Heading:     state.Heading,
			LastUpdated: state.LastUpdated.UTC(),
		}

		// Append it to the list
		pilots = append(pilots, p)

		// Add this CID to the unique CID list
		uniqueCIDs[state.CID] = nil

		return true
	})

	// Build the data feed
	feed := schema{
		General: general{
			Version:          0,
			UpdateTimestamp:  time.Now().UTC(),
			ConnectedClients: len(pilots),
			UniqueUsers:      len(uniqueCIDs),
		},
		Pilots:       pilots,
		Ratings:      s.makeControllerRatingList(),
		PilotRatings: s.makePilotRatingsList(),
	}

	return s.setFeed(feed)
}

func (s *DataFeedService) setFeed(feed schema) (err error) {

	// Marshal result
	var feedBytes []byte
	if feedBytes, err = json.Marshal(feed); err != nil {
		return err
	}

	// Set the new feed
	d := servercontext.DataFeed()
	d.SetFeed(string(feedBytes), feed.General.UpdateTimestamp)

	return nil
}

func (s *DataFeedService) makeControllerRatingList() (ratings []controllerRating) {
	ratings = make([]controllerRating, 0, 14)

	protocol.ForEachNetworkRating(func(id protocol.NetworkRating, shortString string, longString string) {
		ratings = append(ratings, controllerRating{
			ID:        id,
			ShortName: shortString,
			LongName:  longString,
		})
	})

	return ratings
}

func (s *DataFeedService) makePilotRatingsList() (ratings []pilotRating) {
	ratings = make([]pilotRating, 0, 7)

	protocol.ForEachPilotRating(func(id protocol.PilotRating, shortString string, longString string) {
		ratings = append(ratings, pilotRating{
			ID:        id,
			ShortName: shortString,
			LongName:  longString,
		})
	})

	return ratings
}
