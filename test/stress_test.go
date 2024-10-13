package test

import (
	"bytes"
	"context"
	"fmt"
	"github.com/renorris/openfsd/bootstrap"
	"github.com/renorris/openfsd/database"
	"github.com/renorris/openfsd/protocol"
	"github.com/renorris/openfsd/servercontext"
	"github.com/stretchr/testify/assert"
	"io"
	"log"
	"math/rand"
	"net"
	"os"
	"sync"
	"testing"
	"time"
)

func TestStressTest(t *testing.T) {
	// Setup config for testing environment
	if err := os.Setenv("IN_MEMORY_DB", "true"); err != nil {
		t.Fatal(err)
	}

	// Start the server
	ctx, cancelCtx := context.WithCancel(context.Background())
	b := bootstrap.NewDefaultBootstrap()
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}

	defer func() {
		cancelCtx()
		<-b.Done
	}()

	user := database.FSDUserRecord{
		Email:         "example@mail.com",
		FirstName:     "Test User",
		LastName:      "Test User",
		Password:      "54321",
		FSDPassword:   "12345",
		NetworkRating: protocol.NetworkRatingOBS,
		PilotRating:   0,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	var err error
	user.CID, err = user.Insert(servercontext.DB())
	assert.Nil(t, err)

	wg := sync.WaitGroup{}
	for ci := 100001; ci < 100128; ci++ {
		wg.Add(1)
		time.Sleep(20 * time.Millisecond)
		go func(ci int) {
			defer wg.Done()

			log.Printf("Connecting %d", ci)

			var conn net.Conn
			if conn, err = net.Dial("tcp4", "localhost:6809"); err != nil {
				t.Fatal(err)
			}

			go func() {
				io.Copy(io.Discard, conn)
			}()

			if _, err = io.Copy(conn, bytes.NewReader([]byte(fmt.Sprintf("$ID%d:SERVER:88e4:vPilot:3:8:%d:99999:1234567890\r\n", ci, user.CID)))); err != nil {
				t.Fatal(err)
			}

			if _, err = io.Copy(conn, bytes.NewReader([]byte(fmt.Sprintf("#AP%d:SERVER:%d:12345:1:101:2:Briner\r\n", ci, user.CID)))); err != nil {
				t.Fatal(err)
			}

			ticker := time.NewTicker(200 * time.Millisecond)
			defer ticker.Stop()

			for i := range 10 {
				randLat := randFloats(-90, 90, 1)[0]
				randLon := randFloats(-180, 180, 1)[0]

				if i%5 == 0 {
					if _, err = io.Copy(conn, bytes.NewReader([]byte(fmt.Sprintf("@S:%d:1200:1:%.6f:%.6f:16:0:4060:336\r\n", ci, randLat, randLon)))); err != nil {
						t.Fatal(err)
					}
				}

				if _, err = io.Copy(conn, bytes.NewReader([]byte(fmt.Sprintf("^%d:%.6f:%.6f:20.20:8.62:944:-0.0001:-0.0017:0.0000:0.0000:0.0000:0.0000:0.00\r\n", ci, randLat, randLon)))); err != nil {
					t.Fatal(err)
				}

				<-ticker.C
			}

			if _, err = io.Copy(conn, bytes.NewReader([]byte(fmt.Sprintf("#DP%d:%d\r\n", ci, user.CID)))); err != nil {
				t.Fatal(err)
			}

		}(ci)
	}

	wg.Wait()
}

func randFloats(min, max float64, n int) []float64 {
	res := make([]float64, n)
	for i := range res {
		res[i] = min + rand.Float64()*(max-min)
	}
	return res
}
