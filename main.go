// Command bus-status shows what Sessionbus peers and lanes are doing.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

func main() { os.Exit(run(os.Args[1:])) }
func run(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "serve":
			return runServe(args[1:])
		case "watch":
			args = args[1:]
		default:
			if !strings.HasPrefix(args[0], "-") {
				fmt.Fprintln(os.Stderr, "bus-status: unknown command:", args[0])
				return 2
			}
		}
	}
	return runWatch(args)
}

// TIOCGWINSZ succeeds for a terminal, unlike a character-device check (which
// would also accept /dev/null). This viewer targets Unix Sessionbus hosts.
func isTerminal(file *os.File) bool {
	var size [4]uint16
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&size)))
	return err == 0
}
func runWatch(args []string) int {
	flags := flag.NewFlagSet("watch", flag.ContinueOnError)
	interval := flags.Duration("interval", 2*time.Second, "refresh interval")
	once := flags.Bool("once", false, "print one frame")
	path := flags.String("state", defaultStatePath(), "state file")
	roster := flags.String("roster", "sessionbus roster --json", "roster command (space-separated)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	command := strings.Fields(*roster)
	if flags.NArg() != 0 || len(command) == 0 || *interval <= 0 {
		fmt.Fprintln(os.Stderr, "bus-status: expected a roster command and a positive interval")
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	_, noColor := os.LookupEnv("NO_COLOR")
	color := !noColor && isTerminal(os.Stdout)
	var prev map[string]bool
	for {
		if ctx.Err() != nil {
			return 0
		}
		cmd := exec.CommandContext(ctx, command[0], command[1:]...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		runErr := cmd.Run()
		if ctx.Err() != nil {
			return 0
		}
		state, loadErr := loadState(*path)
		now := time.Now()
		frame := Frame(stdout.Bytes(), stderr.Bytes(), runErr, state, now, color)
		if loadErr != nil {
			frame += "Ошибка state: " + sanitize(loadErr.Error()) + "\n"
		}
		report, decodeErr := decodeReport(stdout.Bytes())
		if *once {
			fmt.Fprint(os.Stdout, frame)
			if decodeErr != nil {
				return 1
			}
			return 0
		}
		fmt.Fprint(os.Stdout, "\x1b[H\x1b[2J", frame)
		cur := askIDs(report, state)
		if newAsks(prev, cur) {
			fmt.Fprint(os.Stdout, "\a")
		}
		prev = cur
		timer := time.NewTimer(*interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return 0
		case <-timer.C:
		}
	}
}
