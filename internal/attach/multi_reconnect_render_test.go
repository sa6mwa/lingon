package attach_test

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/backoff"
	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/terminal"
)

func TestMultiAttachRendersAfterReconnect(t *testing.T) {
	recorder := ptytest.NewWSRecorder()
	h := newHarness(t, ptytest.WithWSRecorder(recorder))
	var viewClient *attach.Client
	var frameCount int32
	var readyCount int32
	var viewCount int32
	var reconnectCount int32
	var reconnectMu sync.Mutex
	var reconnectAttempts []int
	var closedMu sync.Mutex
	var closedStates []string
	var clientPayloadsMu sync.Mutex
	var clientPayloads []string
	var sendHelloMu sync.Mutex
	var sendHelloErrs []string
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "session_reconnect",
		SessionName: "session_reconnect",
		Shell:       "/bin/sh",
		Cols:        80,
		Rows:        24,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"session_reconnect"})
	waitForHost(t, h, "session_reconnect", 3*time.Second)

	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "session_reconnect",
		Cols:      80,
		Rows:      24,
		BackoffPolicy: backoff.Policy{
			Base:   100 * time.Millisecond,
			Factor: 2,
			Max:    1 * time.Second,
		},
		OnReconnect: func(id string, attempt int) {
			if id != "session_reconnect" {
				return
			}
			atomic.AddInt32(&reconnectCount, 1)
			reconnectMu.Lock()
			reconnectAttempts = append(reconnectAttempts, attempt)
			reconnectMu.Unlock()
		},
		OnViewClosed: func(id string, visible bool, current bool) {
			if id != "session_reconnect" {
				return
			}
			closedMu.Lock()
			closedStates = append(closedStates, fmt.Sprintf("visible=%t current=%t", visible, current))
			closedMu.Unlock()
		},
		OnView: func(id string, client *attach.Client) {
			if id == "session_reconnect" {
				atomic.AddInt32(&viewCount, 1)
				viewClient = client
				client.OnFrame = func(frame *protocolpb.Frame) {
					atomic.AddInt32(&frameCount, 1)
					clientPayloadsMu.Lock()
					clientPayloads = append(clientPayloads, framePayload(frame))
					clientPayloadsMu.Unlock()
				}
				client.OnSendHello = func(err error) {
					sendHelloMu.Lock()
					if err != nil {
						sendHelloErrs = append(sendHelloErrs, err.Error())
					} else {
						sendHelloErrs = append(sendHelloErrs, "<nil>")
					}
					sendHelloMu.Unlock()
				}
				origReady := client.OnReady
				client.OnReady = func() {
					if origReady != nil {
						origReady()
					}
					atomic.AddInt32(&readyCount, 1)
				}
			}
		},
	})
	t.Cleanup(attachSess.Cancel)
	if exited, err := attachSess.WaitErr(200 * time.Millisecond); exited {
		t.Fatalf("attach exited early: %v", err)
	}
	waitForClientCount(t, h, "session_reconnect", 1, 3*time.Second)
	waitForFramePayload(t, h.Clock(), recorder, "client", "session_reconnect", ptytest.DirServerToClient, "welcome", 1, 3*time.Second)
	attachSess.DrainRaw()

	host.Send("printf '\\n\\nbefore-reconnect\\n'\n")
	if exited, err := attachSess.WaitErr(50 * time.Millisecond); exited {
		t.Fatalf("attach exited before render: %v", err)
	}
	host.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("before-reconnect") {
			return ptytest.FormatRowDiff("host", 0, screen.Row(0))
		}
		return nil
	})
	waitForSessionSeq(t, h, "session_reconnect", 1, 3*time.Second)
	waitForFramePayload(t, h.Clock(), recorder, "client", "session_reconnect", ptytest.DirServerToClient, "snapshot", 1, 3*time.Second)
	if h.ClientCount("session_reconnect") == 0 {
		t.Fatalf("expected attached client to remain connected after host output")
	}
	attachSess.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("before-reconnect") {
			raw := attachSess.DrainRaw()
			frames := recorder.Frames()
			var snapCount, diffCount, errCount int
			hasSnapshotText := false
			hasDiffText := false
			diffRows := map[int]struct{}{}
			var snapSeqs []uint64
			var diffSeqs []uint64
			var snapDims [][2]uint32
			var diffDims [][2]uint32
			var helloCount int
			var welcomeCount int
			var hiddenModes int
			for _, rec := range frames {
				if rec.SessionID != "session_reconnect" {
					continue
				}
				if rec.Direction == ptytest.DirClientToServer && rec.Payload == "hello" {
					helloCount++
				}
				if rec.Direction == ptytest.DirServerToClient && rec.Payload == "welcome" {
					welcomeCount++
				}
				if rec.Direction != ptytest.DirServerToClient {
					continue
				}
				switch rec.Payload {
				case "snapshot":
					snapCount++
					snapSeqs = append(snapSeqs, rec.Seq)
					if snap := parseSnapshot(rec.Raw); snap != nil {
						snapDims = append(snapDims, [2]uint32{snap.Cols, snap.Rows})
						for _, mode := range snap.Modes {
							if int16(mode)&terminal.ModeHidden != 0 {
								hiddenModes++
							}
						}
					}
					if frameHasText(rec.Raw, "before-reconnect") {
						hasSnapshotText = true
					}
				case "diff":
					diffCount++
					diffSeqs = append(diffSeqs, rec.Seq)
					if diff := parseDiff(rec.Raw); diff != nil {
						diffDims = append(diffDims, [2]uint32{diff.Cols, diff.Rows})
						for _, row := range diff.DiffRows {
							for _, mode := range row.Modes {
								if int16(mode)&terminal.ModeHidden != 0 {
									hiddenModes++
								}
							}
						}
					}
					if rows := diffTextRows(rec.Raw, "before-reconnect"); len(rows) > 0 {
						hasDiffText = true
						for _, row := range rows {
							diffRows[row] = struct{}{}
						}
					}
				case "error":
					errCount++
				}
			}
			cursor := attachSess.Cursor()
			readErr := attachSess.ReadErr()
			clientSnap := (*protocolpb.Snapshot)(nil)
			clientHasText := false
			clientConnected := false
			clientReadErr := error(nil)
			if viewClient != nil {
				clientConnected = viewClient.Connected()
				clientReadErr = viewClient.ReadErr()
				clientSnap = viewClient.Snapshot()
				if clientSnap != nil {
					var buf bytes.Buffer
					_ = mvu.RenderSnapshotViewport(&buf, clientSnap, int(clientSnap.Cols), int(clientSnap.Rows))
					clientHasText = bytes.Contains(buf.Bytes(), []byte("before-reconnect"))
				}
			}
			clientPayloadsMu.Lock()
			payloads := append([]string(nil), clientPayloads...)
			clientPayloadsMu.Unlock()
			sendHelloMu.Lock()
			helloErrs := append([]string(nil), sendHelloErrs...)
			sendHelloMu.Unlock()
			return fmt.Errorf(
				"missing before-reconnect; screen:\n%s\nraw:\n%q\nframes=%d snapshots=%d diffs=%d errors=%d snapshotText=%t diffText=%t diffRows=%v snapSeqs=%v snapDims=%v diffSeqs=%v diffDims=%v hello=%d welcome=%d hidden=%d cursor=%v readErr=%v clientSnap=%t clientText=%t clientConnected=%t clientFrames=%d clientReady=%d clientReadErr=%v clientPayloads=%v sendHelloErrs=%v\ntrace=%s",
				screen.String(), raw, len(frames), snapCount, diffCount, errCount, hasSnapshotText, hasDiffText, sortedRows(diffRows), snapSeqs, snapDims, diffSeqs, diffDims, helloCount, welcomeCount, hiddenModes, cursor, readErr, clientSnap != nil, clientHasText, clientConnected, atomic.LoadInt32(&frameCount), atomic.LoadInt32(&readyCount), clientReadErr, payloads, helloErrs, frameTrace(frames, "session_reconnect"),
			)
		}
		return nil
	})

	h.StopServer()
	h.RestartServer()
	if ok, err := attachSess.WaitErr(2 * time.Second); ok {
		if err != nil && err != context.Canceled && !strings.Contains(err.Error(), "no sessions available") {
			t.Fatalf("attach exit error: %v", err)
		}
		return
	}
	waitForHost(t, h, "session_reconnect", 15*time.Second)
	waitForClient := func(timeout time.Duration) {
		deadline := ptytest.Now(h.Clock()).Add(timeout)
		var stableSince time.Time
		for ptytest.Now(h.Clock()).Before(deadline) {
			if h.ClientCount("session_reconnect") >= 1 {
				if stableSince.IsZero() {
					stableSince = ptytest.Now(h.Clock())
				} else if ptytest.Now(h.Clock()).Sub(stableSince) >= 300*time.Millisecond {
					return
				}
			} else {
				stableSince = time.Time{}
			}
			h.Advance(100 * time.Millisecond)
		}
		var viewConnected bool
		var viewReadErr error
		if viewClient != nil {
			viewConnected = viewClient.Connected()
			viewReadErr = viewClient.ReadErr()
		}
		t.Fatalf("timed out waiting for client reconnect (views=%d connected=%t readErr=%v payloads=%v reconnectAttempts=%v closedStates=%v sendHello=%v)", atomic.LoadInt32(&viewCount), viewConnected, viewReadErr, func() []string {
			clientPayloadsMu.Lock()
			defer clientPayloadsMu.Unlock()
			return append([]string(nil), clientPayloads...)
		}(), func() []int {
			reconnectMu.Lock()
			defer reconnectMu.Unlock()
			return append([]int(nil), reconnectAttempts...)
		}(), func() []string {
			closedMu.Lock()
			defer closedMu.Unlock()
			return append([]string(nil), closedStates...)
		}(), func() []string {
			sendHelloMu.Lock()
			defer sendHelloMu.Unlock()
			return append([]string(nil), sendHelloErrs...)
		}())
	}
	waitForClient(15 * time.Second)
	waitForFramePayload(t, h.Clock(), recorder, "client", "session_reconnect", ptytest.DirServerToClient, "welcome", 2, 15*time.Second)
	if exited, err := attachSess.WaitErr(50 * time.Millisecond); exited {
		t.Fatalf("attach exited after reconnect: %v", err)
	}

	inputCount := 0
	for _, rec := range recorder.Frames() {
		if rec.Role != "client" || rec.Direction != ptytest.DirClientToServer {
			continue
		}
		if rec.Payload == "input" {
			inputCount++
		}
	}
	attachSess.Send("echo after-reconnect\n")
	waitForFramePayload(t, h.Clock(), recorder, "client", "", ptytest.DirClientToServer, "input", inputCount+1, 3*time.Second)
	host.Eventually(5*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("after-reconnect") {
			return ptytest.FormatRowDiff("host", 0, screen.Row(0))
		}
		return nil
	})
	attachSess.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("after-reconnect") {
			return ptytest.FormatRowDiff("attach", 0, screen.Row(0))
		}
		return nil
	})
}

func frameHasText(data []byte, needle string) bool {
	var frame protocolpb.Frame
	if err := proto.Unmarshal(data, &frame); err != nil {
		return false
	}
	snap := frame.GetSnapshot()
	if snap == nil {
		return false
	}
	var buf bytes.Buffer
	_ = mvu.RenderSnapshotViewport(&buf, snap, int(snap.Cols), int(snap.Rows))
	return bytes.Contains(buf.Bytes(), []byte(needle))
}

func framePayload(frame *protocolpb.Frame) string {
	if frame == nil {
		return "nil"
	}
	switch frame.Payload.(type) {
	case *protocolpb.Frame_Snapshot:
		return "snapshot"
	case *protocolpb.Frame_Diff:
		return "diff"
	case *protocolpb.Frame_Hello:
		return "hello"
	case *protocolpb.Frame_Welcome:
		return "welcome"
	case *protocolpb.Frame_Control:
		return "control"
	case *protocolpb.Frame_Error:
		return "error"
	case *protocolpb.Frame_In:
		return "input"
	case *protocolpb.Frame_Resize:
		return "resize"
	default:
		return "other"
	}
}

func parseSnapshot(data []byte) *protocolpb.Snapshot {
	var frame protocolpb.Frame
	if err := proto.Unmarshal(data, &frame); err != nil {
		return nil
	}
	return frame.GetSnapshot()
}

func parseDiff(data []byte) *protocolpb.Diff {
	var frame protocolpb.Frame
	if err := proto.Unmarshal(data, &frame); err != nil {
		return nil
	}
	return frame.GetDiff()
}

func diffTextRows(data []byte, needle string) []int {
	var frame protocolpb.Frame
	if err := proto.Unmarshal(data, &frame); err != nil {
		return nil
	}
	diff := frame.GetDiff()
	if diff == nil {
		return nil
	}
	var rows []int
	for _, row := range diff.DiffRows {
		if len(row.Runes) == 0 {
			continue
		}
		var buf bytes.Buffer
		for _, r := range row.Runes {
			if r == 0 {
				buf.WriteByte(' ')
				continue
			}
			buf.WriteRune(rune(r))
		}
		if bytes.Contains(buf.Bytes(), []byte(needle)) {
			rows = append(rows, int(row.Row))
		}
	}
	return rows
}

func sortedRows(rows map[int]struct{}) []int {
	if len(rows) == 0 {
		return nil
	}
	out := make([]int, 0, len(rows))
	for row := range rows {
		out = append(out, row)
	}
	sort.Ints(out)
	return out
}

func frameTrace(frames []ptytest.FrameRecord, sessionID string) string {
	if len(frames) == 0 {
		return ""
	}
	var buf bytes.Buffer
	for _, rec := range frames {
		if sessionID != "" && rec.SessionID != sessionID {
			continue
		}
		fmt.Fprintf(&buf, "%s %s %s seq=%d\n", rec.Role, rec.Direction, rec.Payload, rec.Seq)
	}
	return strings.TrimSpace(buf.String())
}
