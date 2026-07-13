// Package fsdclient is a protocol-faithful FSD TCP client for openfsd tests
// and tooling.
//
// It depends only on pkg/protocol and the Go standard library (no internal/*
// imports). Dial expects the server's $DISERVER:CLIENT identification packet,
// LoginPilot/LoginATC send optional $ID plus #AP/#AA (token field holds a
// password or JWT), and position helpers cover protocol 100 (@) plus thin
// protocol 101 wrappers.
//
// Concurrency: each Client is independent (N clients OK). Send is safe for
// concurrent use; Next is single-flight per client. Recorder is safe for
// concurrent inspection; use Client.WaitFor to read until a predicate matches,
// or pump Next in a goroutine and use Recorder.WaitFor.
package fsdclient
