package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"time"
	"unicode/utf8"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/platform"
)

var _ adapter.Renewer = (*Adapter)(nil)

// maxReason caps the failure reason so a chatty server cannot flood a finding.
const maxReason = 200

// maxLine bounds one JSON line from the app server.
const maxLine = 1 << 20

// exitGrace is how long a server that has answered gets to exit on stdin EOF
// before it is killed.
const exitGrace = 2 * time.Second

// renewWaitDelay bounds how long Wait lingers on I/O after the process is killed.
const renewWaitDelay = 2 * time.Second

// renewDropped are variables that would make codex authenticate some other
// way than the account's own login, so they never reach the child.
var renewDropped = []string{"CODEX_HOME", "OPENAI_API_KEY", "CODEX_API_KEY", "OPENAI_BASE_URL"}

type rpcMessage struct {
	ID     *int            `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Renew starts `codex app-server` against acct's codex home and asks it to
// read the account with refreshToken set, so that Codex renews the login and
// rewrites auth.json itself. modelpick writes nothing of its own. The caller
// re-reads the credential to learn whether it worked, so success here is only
// "the server answered account/read".
func (a *Adapter) Renew(ctx context.Context, acct adapter.Account) error {
	home, err := filepath.Abs(acct.CodexHome)
	if err != nil {
		return fmt.Errorf("codex: codex home: %w", err)
	}
	argv := a.o.AppServer
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = append(platform.EnvWithout(a.o.Environ(), renewDropped...), "CODEX_HOME="+home)
	cmd.WaitDelay = renewWaitDelay
	platform.NewProcessGroup(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("codex: app-server: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("codex: app-server: %w", err)
	}
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
			return errors.New("codex: codex not found on PATH")
		}
		return fmt.Errorf("codex: app-server: %w", err)
	}

	rerr := a.converse(ctx, stdin, bufio.NewScanner(stdout))
	stdin.Close()
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	if rerr == nil {
		// The server should exit on stdin EOF; if it lingers, the answer is
		// already in hand, so its exit status is not part of the result.
		select {
		case <-done:
			return nil
		case <-time.After(exitGrace):
		}
	}
	cmd.Process.Kill()
	<-done
	return rerr
}

// converse runs the handshake and the refreshing account/read.
func (a *Adapter) converse(ctx context.Context, w interface{ Write([]byte) (int, error) }, sc *bufio.Scanner) error {
	sc.Buffer(nil, maxLine)
	send := func(v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if _, err := w.Write(append(b, '\n')); err != nil {
			if cerr := ctx.Err(); cerr != nil {
				return fmt.Errorf("codex app-server: %w", cerr)
			}
			return fmt.Errorf("codex app-server: write: %w", err)
		}
		return nil
	}
	type obj = map[string]any
	if err := send(obj{"id": 0, "method": "initialize", "params": obj{"clientInfo": obj{"name": "modelpick", "version": a.o.Version}}}); err != nil {
		return err
	}
	if _, err := a.await(ctx, sc, 0); err != nil {
		return err
	}
	if err := send(obj{"method": "initialized"}); err != nil {
		return err
	}
	if err := send(obj{"id": 1, "method": "account/read", "params": obj{"refreshToken": true}}); err != nil {
		return err
	}
	_, err := a.await(ctx, sc, 1)
	return err
}

// await reads lines until the response with the given id. Notifications and
// server requests and responses to other ids are skipped; a line's raw text never reaches an error.
func (a *Adapter) await(ctx context.Context, sc *bufio.Scanner, id int) (json.RawMessage, error) {
	for sc.Scan() {
		var m rpcMessage
		if json.Unmarshal(sc.Bytes(), &m) != nil || m.ID == nil || *m.ID != id || m.Method != "" {
			continue
		}
		if m.Error == nil && m.Result == nil {
			continue
		}
		if m.Error != nil {
			return nil, errors.New(clip("codex app-server: "+m.Error.Message, maxReason))
		}
		return m.Result, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("codex app-server: %w", err)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("codex app-server: read: %w", err)
	}
	return nil, errors.New("codex app-server: exited before answering")
}

// clip cuts s to at most n bytes without splitting a UTF-8 sequence.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
