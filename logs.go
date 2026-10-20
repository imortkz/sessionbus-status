package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// LocateLog only searches the supplied home. Callers reject non-local roster rows.
func LocateLog(home, sessionID, product string) (string, string, error) {
	id, host, _ := strings.Cut(sessionID, "@")
	if host != "" {
		local, err := os.Hostname()
		if err != nil || host != local {
			return "", "", fmt.Errorf("remote-host log unavailable: %s", cleanText(host, 120))
		}
	}
	if id == "" || strings.ContainsAny(id, "/\\*?[]") || id == ".." {
		return "", "", fmt.Errorf("invalid session id")
	}
	patterns := map[string][]string{
		"claude": {filepath.Join(home, ".claude/projects/*", id+".jsonl")},
		"codex":  {filepath.Join(home, ".codex/sessions/*/*/*", "rollout-*"+id+".jsonl"), filepath.Join(home, ".codex/archived_sessions", "rollout-*"+id+".jsonl")},
		"dsh":    {filepath.Join(home, ".dsh/sessions/*", id, "session.v3.jsonl.zstd")},
	}
	order := []string{"claude", "codex", "dsh"}
	preferred := strings.TrimSuffix(product, "-peer")
	if product == "sessionbus-dsh" || strings.HasPrefix(id, "session-") {
		preferred = "dsh"
	}
	if _, ok := patterns[preferred]; ok {
		order = []string{preferred}
		for _, k := range []string{"claude", "codex", "dsh"} {
			if k != preferred {
				order = append(order, k)
			}
		}
	}
	for _, kind := range order {
		for _, p := range patterns[kind] {
			matches, err := filepath.Glob(p)
			if err != nil {
				return "", "", err
			}
			if len(matches) > 0 {
				return matches[len(matches)-1], kind, nil
			}
		}
	}
	return "", "", fmt.Errorf("native log not found (or unknown product)")
}

type LogLine struct {
	At         time.Time
	Kind, Text string
}
type LogReader struct {
	path, kind string
	offset     int64
	seen       int64
	size       int64
	info       os.FileInfo
}

func (r *LogReader) Read() ([]LogLine, error) {
	f, err := os.Open(r.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() < r.size || (r.info != nil && !os.SameFile(r.info, info)) {
		r.offset = 0
		r.seen = 0
	}
	r.size = info.Size()
	r.info = info
	var data []byte
	if r.kind == "dsh" {
		dec, err := zstd.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer dec.Close()
		if r.seen > 0 {
			if _, err = io.CopyN(io.Discard, dec, r.seen); err != nil {
				if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
					return nil, nil
				}
				return nil, err
			}
		}
		data, err = io.ReadAll(dec)
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
			return nil, err
		}
	} else {
		dropPartial := false
		if r.offset == 0 && info.Size() > 256*1024 {
			r.offset = info.Size() - 256*1024
			if _, err = f.Seek(r.offset-1, io.SeekStart); err != nil {
				return nil, err
			}
			var prev [1]byte
			if _, err = f.Read(prev[:]); err != nil {
				return nil, err
			}
			dropPartial = prev[0] != '\n'
		}
		if _, err = f.Seek(r.offset, io.SeekStart); err != nil {
			return nil, err
		}
		data, err = io.ReadAll(f)
		if err != nil {
			return nil, err
		}
		if dropPartial {
			i := bytes.IndexByte(data, '\n')
			if i < 0 {
				r.offset += int64(len(data))
				return nil, nil
			}
			r.offset += int64(i + 1)
			data = data[i+1:]
		}
	}
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		return nil, nil
	}
	data = data[:end+1]
	var out []LogLine
	records := bytes.Split(data, []byte{'\n'})
	records = records[:len(records)-1]
	if r.kind == "dsh" {
		r.seen += int64(len(data))
	} else {
		r.offset += int64(len(data))
	}
	for _, record := range records {
		out = append(out, parseLog(r.kind, record)...)
	}
	return out, nil
}
func logLine(kind, text, stamp string) LogLine {
	limit := 200
	switch kind {
	case "TEXT":
		limit = 300
	case "TOOL":
		limit = 120
	case "RESULT":
		limit = 140
	}
	at, _ := time.Parse(time.RFC3339Nano, stamp)
	return LogLine{at, kind, cleanText(text, limit)}
}
func object(v any) map[string]any { m, _ := v.(map[string]any); return m }
func str(v any) string            { s, _ := v.(string); return s }
func parts(v any) []any           { p, _ := v.([]any); return p }
func contentText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	var out []string
	for _, p := range parts(v) {
		if s := str(object(p)["text"]); s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, " ")
}
func argumentText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}
func parseLog(kind string, b []byte) []LogLine {
	var o map[string]any
	if json.Unmarshal(b, &o) != nil {
		return nil
	}
	typ := str(o["type"])
	stamp := str(o["timestamp"])
	var out []LogLine
	emit := func(k, s string) {
		line := logLine(k, s, stamp)
		if line.Text != "" {
			out = append(out, line)
		}
	}
	switch kind {
	case "claude":
		if typ != "assistant" && typ != "user" {
			return nil
		}
		c := object(o["message"])["content"]
		if s, ok := c.(string); ok {
			if typ == "user" {
				emit("SYS", "prompt: "+s)
			}
			return out
		}
		for _, raw := range parts(c) {
			p := object(raw)
			switch str(p["type"]) {
			case "text":
				if typ == "assistant" {
					emit("TEXT", str(p["text"]))
				} else {
					emit("SYS", "prompt: "+str(p["text"]))
				}
			case "thinking":
				emit("THINK", str(p["thinking"]))
			case "tool_use":
				inp := object(p["input"])
				key := ""
				for _, k := range []string{"command", "file_path", "pattern", "query"} {
					if key = str(inp[k]); key != "" {
						break
					}
				}
				emit("TOOL", str(p["name"])+" "+key)
			case "tool_result":
				emit("RESULT", contentText(p["content"]))
			}
		}
	case "codex":
		p := object(o["payload"])
		pt := str(p["type"])
		if typ == "event_msg" {
			switch pt {
			case "thread_settings_applied":
				if m := str(object(p["thread_settings"])["model"]); m != "" {
					emit("SYS", "model="+m)
				}
			case "agent_message":
				emit("TEXT", str(p["message"]))
			case "user_message":
				emit("SYS", "prompt: "+str(p["message"]))
			case "agent_reasoning":
				emit("THINK", str(p["text"]))
			}
		} else if typ == "response_item" {
			switch pt {
			case "function_call", "custom_tool_call":
				args := p["arguments"]
				if args == nil {
					args = p["input"]
				}
				emit("TOOL", str(p["name"])+" "+argumentText(args))
			case "function_call_output", "custom_tool_call_output":
				emit("RESULT", contentText(p["output"]))
			}
		}
	case "dsh":
		d := object(o["data"])
		c := object(d["message"])["content"]
		if typ == "user/message" {
			c = d["content"]
		}
		for _, raw := range parts(c) {
			p := object(raw)
			switch str(p["type"]) {
			case "text":
				if typ == "assistant/message" {
					emit("TEXT", str(p["text"]))
				} else {
					emit("SYS", "prompt: "+str(p["text"]))
				}
			case "reasoning":
				s := str(p["text"])
				if s == "" {
					s = str(p["reasoning"])
				}
				emit("THINK", s)
			case "tool-call":
				emit("TOOL", str(p["name"])+" "+argumentText(p["arguments"]))
			case "tool-result":
				emit("RESULT", contentText(p["content"]))
			}
		}
	}
	return out
}

// LogActive returns the log modification time when it falls within the activity window.
// A missing or unreadable log returns an error; an older log returns a zero time.
func LogActive(home, id, product string, now time.Time, window time.Duration) (time.Time, error) {
	p, _, err := LocateLog(home, id, product)
	if err != nil {
		return time.Time{}, err
	}
	info, err := os.Stat(p)
	if err != nil {
		return time.Time{}, err
	}
	at := info.ModTime()
	if at.After(now) || now.Sub(at) > window {
		return time.Time{}, nil
	}
	return at, nil
}
