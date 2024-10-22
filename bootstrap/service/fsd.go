package service

import (
	"context"
	"errors"
	"github.com/renorris/openfsd/client"
	"github.com/renorris/openfsd/servercontext"
	"log"
	"net"
	"os"
	"slices"
	"sync"
)

type FSDService struct{}

func (s *FSDService) Start(ctx context.Context, doneErr chan<- error) (err error) {

	// Set "AlwaysImmediate" to true in test cases
	if slices.Contains(os.Environ(), "ALWAYS_IMMEDIATE=true") {
		client.AlwaysImmediate = true
	}

	// Attempt to listen. Once a listener is acquired, we can guarantee that clients can connect.
	var listener *net.TCPListener
	if listener, err = s.resolveAndListen(); err != nil {
		return err
	}

	log.Printf("FSD server listening on %s", servercontext.Config().FSDListenAddress)

	// boot FSD on its own goroutine
	go func(ctx context.Context, doneErr chan<- error, listener *net.TCPListener) {
		doneErr <- s.boot(ctx, listener)
	}(ctx, doneErr, listener)

	return nil
}

func (s *FSDService) boot(ctx context.Context, listener *net.TCPListener) error {
	defer listener.Close()
	defer log.Println("FSD server shutting down...")

	incomingConns := make(chan *net.TCPConn)
	go acceptorWorker(ctx, listener, incomingConns)

	// Track each client connection in a wait group
	waitGroup := sync.WaitGroup{}

	for {
		select {
		// Handle incoming connections
		case conn, ok := <-incomingConns:
			if !ok {
				return errors.New("listener closed")
			}

			waitGroup.Add(1)
			go func(ctx context.Context, conn *net.TCPConn) {
				defer waitGroup.Done()

				// Recover from a panic
				defer func() {
					if err := recover(); err != nil {
						log.Println("panic occurred:", err)
					}
				}()

				var connection *client.Connection
				var err error
				if connection, err = client.NewConnection(ctx, conn); err != nil {
					return
				}

				connection.Start()
			}(ctx, conn)

		// Wait for cleanup on context done
		case <-ctx.Done():
			waitGroup.Wait()
			return nil
		}
	}
}

func acceptorWorker(ctx context.Context, listener *net.TCPListener, conns chan<- *net.TCPConn) {
	defer close(conns)
	for {
		conn, err := listener.AcceptTCP()
		if err != nil {
			return
		}

		select {
		case conns <- conn:
		case <-ctx.Done():
			return
		}
	}
}

func (s *FSDService) resolveAndListen() (listener *net.TCPListener, err error) {
	addr, err := net.ResolveTCPAddr("tcp4", servercontext.Config().FSDListenAddress)
	if err != nil {
		return nil, err
	}

	listener, err = net.ListenTCP("tcp4", addr)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	return listener, nil
}
