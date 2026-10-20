package main

import (
	"bytes"
	"context"
	"fmt"
	sdk "github.com/antst/sessionbus/bus/sdk/go"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func delivery(t *testing.T, c *collector, id, body string) {
	t.Helper()
	receipt, err := c.deliver(context.Background(), sdk.PeerIdentity{}, sdk.DeliveryRequest{From: sdk.DeliverySource{SessionID: id, Product: "codex-peer"}, Body: body})
	if err != nil || receipt.Disposition != "written" {
		t.Fatalf("receipt: %+v %v", receipt, err)
	}
}
func TestDeliverStoresAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	state, _ := loadState(path)
	c := &collector{path: path, state: state}
	delivery(t, c, "a", "status: coding\nmodel: gpt")
	delivery(t, c, "a", "ask: approve")
	rec := c.state.Sessions["a"]
	if len(c.state.Sessions) != 1 || rec.Status != "coding" || rec.Model != "gpt" || rec.Ask != "approve" || rec.Product != "codex-peer" || rec.UpdatedAt.IsZero() {
		t.Fatalf("record %+v", rec)
	}
	saved, err := loadState(path)
	if err != nil || !reflect.DeepEqual(saved, c.state) {
		t.Fatalf("saved %+v %v", saved, err)
	}
	before, _ := os.ReadFile(path)
	delivery(t, c, "b", "nothing known")
	after, _ := os.ReadFile(path)
	if string(before) != string(after) || len(c.state.Sessions) != 1 {
		t.Fatal("unknown body mutated state")
	}
}
func TestDeliverSendersAndSaveFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	state, _ := loadState(path)
	c := &collector{path: path, state: state}
	delivery(t, c, "a", "status: first")
	delivery(t, c, "b", "status: second")
	if len(c.state.Sessions) != 2 || c.state.Sessions["a"].Status != "first" || c.state.Sessions["b"].Status != "second" {
		t.Fatal(c.state)
	}
	c.path = filepath.Join(path, "child")
	delivery(t, c, "a", "ask: still accepted")
	if c.state.Sessions["a"].Ask != "still accepted" {
		t.Fatal("failed save lost memory update")
	}
}

func TestCollectorRecoversBadState(t *testing.T) {
	for _, unreadable := range []bool{false, true} {
		t.Run(fmt.Sprint(unreadable), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			original := []byte(`{"version":1,"sessions":{"partial":{"status":"never trust"}},"broken":`)
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			if unreadable {
				if err := os.Chmod(path, 0000); err != nil {
					t.Fatal(err)
				}
			}
			var warnings bytes.Buffer
			state := loadCollectorState(path, &warnings)
			if state.Version != 1 || len(state.Sessions) != 0 {
				t.Fatalf("recovery state %+v", state)
			}
			if !strings.Contains(warnings.String(), "warning") {
				t.Fatalf("no warning: %q", warnings.String())
			}
			backups, err := filepath.Glob(path + ".corrupt-*")
			if err != nil || len(backups) != 1 {
				t.Fatalf("backups %v %v", backups, err)
			}
			if err := os.Chmod(backups[0], 0600); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(backups[0])
			if err != nil || !bytes.Equal(got, original) {
				t.Fatalf("backup: %q %v", got, err)
			}
			c := &collector{path: path, state: state}
			delivery(t, c, "fresh", "status: recovered")
			saved, err := loadState(path)
			if err != nil || saved.Sessions["fresh"].Status != "recovered" {
				t.Fatalf("recovery not writable: %+v %v", saved, err)
			}
		})
	}
}
