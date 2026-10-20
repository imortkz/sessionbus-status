package main

import (
	"context"
	"flag"
	"fmt"
	sdk "github.com/antst/sessionbus/bus/sdk/go"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

type collector struct {
	path  string
	mu    sync.Mutex
	state State
}

func (c *collector) deliver(_ context.Context, _ sdk.PeerIdentity, req sdk.DeliveryRequest) (sdk.DeliveryReceipt, error) {
	updates := ParseBody(req.Body)
	if len(updates) > 0 {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.state.Sessions == nil {
			c.state.Sessions = make(map[string]Record)
		}
		rec := Apply(c.state.Sessions[req.From.SessionID], updates, time.Now().UTC())
		rec.Product = sanitize(req.From.Product)
		c.state.Sessions[req.From.SessionID] = rec
		c.state.Version = 2
		if err := saveState(c.path, c.state); err != nil {
			fmt.Fprintln(os.Stderr, "bus-status: save:", err)
		}
	}
	return sdk.DeliveryReceipt{Disposition: "written"}, nil
}
func runServe(args []string) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	groups := flags.String("g", "bus-status", "comma-separated shared groups")
	path := flags.String("state", defaultStatePath(), "state file")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "bus-status: unexpected serve arguments")
		return 2
	}
	state := loadCollectorState(*path, os.Stderr)
	c := &collector{path: *path, state: state}
	var shared []string
	for _, g := range strings.Split(*groups, ",") {
		if g = strings.TrimSpace(g); g != "" {
			shared = append(shared, g)
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	peer, err := sdk.ConnectPeerContext(ctx, sdk.PeerIdentity{Product: "bus-status", SessionID: "bus-status", Name: "bus-status", Groups: shared, Info: map[string]any{}}, c.deliver)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bus-status:", err)
		return 1
	}
	defer peer.Shutdown()
	ready := peer.Ready()
	for {
		select {
		case <-ready:
			fmt.Fprintln(os.Stderr, "bus-status: connected")
			ready = nil
		case <-ctx.Done():
			peer.Shutdown()
			return 0
		case <-peer.Closed():
			if ctx.Err() != nil {
				return 0
			}
			fmt.Fprintln(os.Stderr, "bus-status:", peer.Err())
			return 1
		}
	}
}

func loadCollectorState(path string, warnings io.Writer) State {
	state, err := loadState(path)
	if err == nil {
		return state
	}
	fmt.Fprintln(warnings, "bus-status: warning: cannot load state:", err)
	backup := fmt.Sprintf("%s.corrupt-%d", path, time.Now().Unix())
	if err := os.Rename(path, backup); err != nil {
		fmt.Fprintln(warnings, "bus-status: warning: cannot preserve bad state:", err)
	}
	return State{Version: 1, Sessions: make(map[string]Record)}
}
