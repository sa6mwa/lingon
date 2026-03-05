package lingon

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/pslog"
)

// SendInputOptions configures a non-interactive attach input session.
type SendInputOptions struct {
	Endpoint       string
	UnixSocket     string
	SessionID      string
	AccessToken    string
	ShareToken     string
	RequestControl bool
	Tokens         []string
	NoNewline      bool
	TLSDir         string
	Insecure       bool
	Logger         pslog.Logger
}

// SendInput connects to a session and sends input tokens, then disconnects.
func SendInput(ctx context.Context, opts SendInputOptions) error {
	if opts.Endpoint == "" && opts.UnixSocket == "" {
		return fmt.Errorf("endpoint is required")
	}
	if opts.SessionID == "" && opts.ShareToken == "" {
		return fmt.Errorf("session id or share token is required")
	}
	if len(opts.Tokens) == 0 {
		return fmt.Errorf("no input tokens provided")
	}
	actions, err := buildInputActions(opts.Tokens, !opts.NoNewline)
	if err != nil {
		return err
	}
	client := &attach.Client{
		Endpoint:           opts.Endpoint,
		SessionID:          opts.SessionID,
		UnixSocket:         opts.UnixSocket,
		AccessToken:        opts.AccessToken,
		ShareToken:         opts.ShareToken,
		RequestControl:     opts.RequestControl,
		AllowOfflineToggle: true,
		TLSDir:             opts.TLSDir,
		Insecure:           opts.Insecure,
		Logger:             opts.Logger,
		Stdout:             io.Discard,
		Stderr:             io.Discard,
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	ready := make(chan struct{})
	client.OnReady = func() {
		select {
		case <-ready:
		default:
			close(ready)
		}
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.RunDetached(runCtx)
	}()

	select {
	case <-ready:
	case err := <-errCh:
		if err != nil {
			return err
		}
		return fmt.Errorf("attach exited before sending input")
	case <-time.After(5 * time.Second):
		cancel()
		select {
		case err := <-errCh:
			if err != nil {
				return err
			}
		case <-time.After(2 * time.Second):
		}
		return fmt.Errorf("attach did not become ready")
	}

	for _, action := range actions {
		if action.wait > 0 {
			time.Sleep(action.wait)
			continue
		}
		if len(action.data) == 0 {
			continue
		}
		if err := client.SendInput(ctx, action.data); err != nil {
			client.Close("send failed")
			cancel()
			select {
			case <-errCh:
			case <-time.After(2 * time.Second):
			}
			return err
		}
	}

	client.Close("send complete")
	select {
	case err := <-errCh:
		if err != nil {
			return err
		}
	case <-time.After(2 * time.Second):
		cancel()
		select {
		case err := <-errCh:
			if err != nil {
				return err
			}
		case <-time.After(2 * time.Second):
		}
	}
	return nil
}

type inputAction struct {
	data []byte
	wait time.Duration
}

func buildInputActions(tokens []string, addNewline bool) ([]inputAction, error) {
	commands := splitCommands(tokens)
	if len(commands) == 0 {
		return nil, fmt.Errorf("no input tokens provided")
	}
	var actions []inputAction
	for _, cmd := range commands {
		cmdActions, err := parseInputText(cmd)
		if err != nil {
			return nil, err
		}
		actions = append(actions, cmdActions...)
		if addNewline {
			actions = append(actions, inputAction{data: []byte("\n")})
		}
	}
	return actions, nil
}

func splitCommands(tokens []string) []string {
	var commands []string
	var current strings.Builder
	for _, token := range tokens {
		parts := strings.Split(token, ";")
		for i, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				if current.Len() > 0 {
					current.WriteByte(' ')
				}
				current.WriteString(part)
			}
			if i < len(parts)-1 {
				cmd := strings.TrimSpace(current.String())
				if cmd != "" {
					commands = append(commands, cmd)
				}
				current.Reset()
			}
		}
	}
	if cmd := strings.TrimSpace(current.String()); cmd != "" {
		commands = append(commands, cmd)
	}
	return commands
}

func parseInputText(text string) ([]inputAction, error) {
	if text == "" {
		return nil, nil
	}
	var actions []inputAction
	var buf bytes.Buffer
	for i := 0; i < len(text); {
		if text[i] != '{' {
			buf.WriteByte(text[i])
			i++
			continue
		}
		end := strings.IndexByte(text[i+1:], '}')
		if end < 0 {
			buf.WriteByte(text[i])
			i++
			continue
		}
		token := strings.TrimSpace(text[i+1 : i+1+end])
		if action, ok := parseTokenAction(token); ok {
			if buf.Len() > 0 {
				actions = append(actions, inputAction{data: append([]byte(nil), buf.Bytes()...)})
				buf.Reset()
			}
			actions = append(actions, action)
			i += end + 2
			continue
		}
		buf.WriteByte(text[i])
		i++
	}
	if buf.Len() > 0 {
		actions = append(actions, inputAction{data: append([]byte(nil), buf.Bytes()...)})
	}
	return actions, nil
}

func parseTokenAction(token string) (inputAction, bool) {
	if token == "" {
		return inputAction{}, false
	}
	if strings.HasPrefix(token, "W") && len(token) > 1 {
		wait, err := time.ParseDuration(strings.TrimSpace(token[1:]))
		if err == nil {
			return inputAction{wait: wait}, true
		}
	}
	if strings.HasPrefix(strings.ToUpper(token), "C-") && len(token) >= 3 {
		r := []rune(token[2:])
		if len(r) == 1 {
			ch := r[0]
			if ch >= 'A' && ch <= 'Z' {
				ch = ch - 'A' + 'a'
			}
			if ch >= 'a' && ch <= 'z' {
				return inputAction{data: []byte{byte(ch - 'a' + 1)}}, true
			}
		}
	}
	switch strings.ToUpper(token) {
	case "ESC":
		return inputAction{data: []byte{0x1b}}, true
	case "ENTER":
		return inputAction{data: []byte("\n")}, true
	case "TAB":
		return inputAction{data: []byte{'\t'}}, true
	case "BS":
		return inputAction{data: []byte{0x08}}, true
	case "DEL":
		return inputAction{data: []byte{0x7f}}, true
	case "UP":
		return inputAction{data: []byte("\x1b[A")}, true
	case "DOWN":
		return inputAction{data: []byte("\x1b[B")}, true
	case "LEFT":
		return inputAction{data: []byte("\x1b[D")}, true
	case "RIGHT":
		return inputAction{data: []byte("\x1b[C")}, true
	}
	return inputAction{}, false
}
