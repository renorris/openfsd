package bootstrap

import (
	"context"
	"errors"
	"github.com/renorris/openfsd/bootstrap/service"
	"github.com/renorris/openfsd/servercontext"
	"reflect"
)

// Bootstrap bootstraps/manages multiple services running concurrently
type Bootstrap struct {
	services   []Service
	startErrCh chan error
	doneErrs   []chan error
	Done       chan struct{}
}

type Service interface {
	// Start is the function call to start a service.
	//
	// Start should return once the service is healthily running.
	// Start should return diligently, blocking for minimal time.
	//
	// A service is expected to promptly shut itself down on ctx close.
	//
	// (It is convention that a service runs concurrently on its own,
	// using ctx as the signal to eventually shut down.)
	// The service must send an error over doneErr when it stops in
	// response to the context closing or due to an internal error.
	Start(ctx context.Context, doneErr chan<- error) error
}

// NewDefaultBootstrap makes a new bootstrapper for the default openfsd services
func NewDefaultBootstrap() *Bootstrap {

	servercontext.InitializeServerContextSingleton(servercontext.New())

	services := []Service{&service.FSDService{}, &service.HTTPService{}, &service.DataFeedService{}}

	if servercontext.Config().InMemoryDB {
		services = append(services, &service.InMemoryDatabaseService{})
	}

	return NewBootstrap(services)
}

func NewBootstrap(services []Service) *Bootstrap {
	return &Bootstrap{
		services:   services,
		startErrCh: make(chan error),
		doneErrs:   make([]chan error, 0),
		Done:       make(chan struct{}),
	}
}

// Start starts the bootstrapping process.
// Returns when all services have started successfully.
func (b *Bootstrap) Start(c context.Context) error {

	ctx, cancel := context.WithCancel(c)

	for _, svc := range b.services {
		doneErr := make(chan error)
		b.doneErrs = append(b.doneErrs, doneErr)
		go func(s Service, doneErr chan error) {
			b.startErrCh <- s.Start(ctx, doneErr)
		}(svc, doneErr)
	}

	// Wait until all services finish starting
	capturedStartErrs := make([]error, 0)
	for range b.services {
		var err error
		if err = <-b.startErrCh; err != nil {
			// Fire cancel so all services spin down
			cancel()
		}
		capturedStartErrs = append(capturedStartErrs, err)
	}

	// Start bootstrap monitor
	go b.monitor(cancel)

	// Return an error if >0 services ready'd with an error
	var errs error
	for _, err := range capturedStartErrs {
		if err != nil {
			errs = errors.Join(errs, err)
		}
	}

	if errs != nil {
		return errs
	}

	return nil
}

func (b *Bootstrap) monitor(cancel func()) {

	// Dynamically select the doneErr channel from each service.
	// If a signal is received, check the error.
	// If non-nil, spin down the other services.
	// If nil, noop.
	// Once the error from each service has been
	// captured, signal that we're closed and return.

	cases := make([]reflect.SelectCase, len(b.doneErrs))
	for i, ch := range b.doneErrs {
		cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ch)}
	}

	capturedDoneSigs := make([]error, 0)

	for {
		i, val, ok := reflect.Select(cases)
		if !ok {
			// Remove this channel from the case list
			cases[i] = cases[len(cases)-1]
			cases = cases[:len(cases)-1]
			continue
		}

		var err error
		if val.IsNil() {
			err = nil
		} else {
			err = val.Interface().(error)
		}
		capturedDoneSigs = append(capturedDoneSigs, err)

		// Spin down all services if this service returned an error
		if err != nil {
			cancel()
		}

		// Check if all services have returned
		if len(capturedDoneSigs) == len(b.doneErrs) {
			// Mark the completion of this bootstrapping
			// process by closing the done channel.
			close(b.Done)
			return
		}
	}
}
