// Command bus-status shows what Sessionbus peers and lanes are doing.
package main

import (
	"fmt"
	"os"

	sdk "github.com/antst/sessionbus/bus/sdk/go"
)

var _ sdk.DeliverFunc

func main() {
	fmt.Fprintln(os.Stderr, "bus-status: not implemented")
	os.Exit(2)
}
