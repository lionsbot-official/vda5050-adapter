package vdaapp

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type busFunc func(context.Context) error

func (f busFunc) Start(ctx context.Context) error { return f(ctx) }

func TestApp_FailFastCancelsPeers(t *testing.T) {
	wantErr := errors.New("bus boom")
	failing := busFunc(func(ctx context.Context) error {
		return wantErr
	})
	stopped := make(chan struct{})
	peer := busFunc(func(ctx context.Context) error {
		<-ctx.Done()
		close(stopped)
		return nil
	})

	err := New(failing, peer).Start(context.Background())
	if !errors.Is(err, wantErr) {
		t.Errorf("Start err = %v, want %v", err, wantErr)
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("peer bus was not cancelled when another bus failed")
	}
}

func TestApp_GracefulShutdownOnCancel(t *testing.T) {
	var stops atomic.Int32
	bus := busFunc(func(ctx context.Context) error {
		<-ctx.Done()
		stops.Add(1)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- New(bus, bus).Start(ctx) }()
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("graceful shutdown err = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after context cancel")
	}
	if n := stops.Load(); n != 2 {
		t.Errorf("stopped buses = %d, want 2", n)
	}
}
