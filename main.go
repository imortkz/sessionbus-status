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
	"sync"
	"syscall"
	"time"
	"unsafe"

	tea "github.com/charmbracelet/bubbletea"
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
	timeout := flags.Duration("roster-timeout", 10*time.Second, "roster command timeout (default max(interval, 10s))")
	once := flags.Bool("once", false, "print one frame")
	path := flags.String("state", defaultStatePath(), "state file")
	groups := flags.String("g", "bus-status", "comma-separated shared groups")
	muted := flags.Bool("no-sound", false, "mute sound and notifications")
	sound := flags.String("sound", "/System/Library/Sounds/Glass.aiff", "notification sound path")
	roster := flags.String("roster", "sessionbus roster --json", "roster command (space-separated)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	timeoutSet := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "roster-timeout" {
			timeoutSet = true
		}
	})
	if !timeoutSet {
		*timeout = max(*interval, 10*time.Second)
	}
	command := strings.Fields(*roster)
	if flags.NArg() != 0 || len(command) == 0 || *interval <= 0 || *timeout <= 0 {
		fmt.Fprintln(os.Stderr, "bus-status: expected a roster command and positive interval/roster-timeout")
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	_, noColor := os.LookupEnv("NO_COLOR")
	color := !noColor && isTerminal(os.Stdout)
	if *once {
		stdout, stderr, runErr := runRoster(ctx, command, *timeout)
		if ctx.Err() != nil {
			return 0
		}
		state, loadErr := loadState(*path)
		frame := Frame(stdout, stderr, runErr, state, time.Now(), color)
		if loadErr != nil {
			frame += uiStateError + sanitize(loadErr.Error()) + "\n"
		}
		fmt.Fprint(os.Stdout, frame)
		if _, err := decodeReport(stdout); err != nil {
			return 1
		}
		return 0
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "bus-status:", err)
		return 1
	}
	wg := &sync.WaitGroup{}
	model := newModel(tuiConfig{color: color, collectorWG: wg, ctx: ctx, cancel: cancel, statePath: *path, home: home, groups: *groups, sound: *sound, command: command, interval: *interval, timeout: *timeout, muted: *muted})
	_, runErr := tea.NewProgram(model, tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	wasCanceled := ctx.Err() != nil
	cancel()
	waitCollector(wg, 2*time.Second)
	if runErr != nil && !wasCanceled {
		fmt.Fprintln(os.Stderr, "bus-status:", runErr)
		return 1
	}

	return 0
}

// Failed roster ticks provide no evidence that an existing ask disappeared.
func nextBell(prev map[string]bool, report Report, decodeErr error, state State) (bool, map[string]bool) {
	if decodeErr != nil {
		return false, prev
	}
	cur := askIDs(report, state)
	return newAsks(prev, cur), cur
}

func runRoster(ctx context.Context, command []string, timeout time.Duration) ([]byte, []byte, error) {
	tickCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(tickCtx, command[0], command[1:]...)
	// Bound pipe draining too if a command leaves descendants holding its pipes.
	cmd.WaitDelay = 100 * time.Millisecond
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if tickCtx.Err() != nil {
		return nil, nil, fmt.Errorf("roster command: %w", tickCtx.Err())
	}
	return stdout.Bytes(), stderr.Bytes(), err
}

func waitCollector(wg *sync.WaitGroup, timeout time.Duration) bool {
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}
