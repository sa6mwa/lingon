package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"strconv"

	"pkt.systems/lingon/internal/config"
	"pkt.systems/lingon/internal/headless"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/sessionmanage"
)

// Manage executes an owner-authorized lifecycle operation on a local session.
func (r *Runner) Manage(ctx context.Context, req sessionmanage.Request) sessionmanage.Result {
	return r.management.Execute(ctx, req, func() sessionmanage.Result {
		local := r.localSession(req.SessionID)
		if local == nil {
			return sessionmanage.Result{Error: "session is unavailable on this host"}
		}
		if req.Kind == "rename" {
			if r.opts.OnRename != nil {
				if err := r.opts.OnRename(local.ID(), req.Name); err != nil {
					return sessionmanage.Result{Error: err.Error()}
				}
			}
			local.nameMu.Lock()
			local.name = req.Name
			local.nameMu.Unlock()
			if local.publisher != nil {
				local.publisher.SetSessionName(req.Name)
				local.publisher.SendManagement(&protocolpb.SessionManagement{Kind: "rename", Name: req.Name, ResultSessionId: local.ID(), Completed: true})
			}
			r.refreshTabBar(r.stdout())
			return sessionmanage.Result{SessionID: local.ID(), Name: req.Name}
		}
		// Stable request-derived IDs prevent concurrent retries from launching separate children.
		sum := sha256.Sum256([]byte(req.SessionID + "\x00" + req.RequestID))
		id := fmt.Sprintf("terminal-%x", sum[:12])
		cfg := r.opts.ConfigDir
		if cfg == "" {
			cfg = config.DefaultConfigDir()
		}
		records, err := headless.NewStore(cfg).Reconcile()
		if err != nil {
			return sessionmanage.Result{Error: err.Error()}
		}
		for _, record := range records {
			if record.SessionID == id {
				return sessionmanage.Result{SessionID: id, Name: record.Name}
			}
		}
		args := []string{"-x", "--config-dir", cfg, "--session", id, "--cols", strconv.Itoa(r.opts.Cols), "--rows", strconv.Itoa(r.opts.Rows), "--term", r.opts.Term, "--respawn=" + strconv.FormatBool(r.opts.Respawn), "--scrollback-lines", strconv.Itoa(r.opts.ScrollbackLines)}
		if r.opts.ConfigFile != "" {
			args = append(args, "--config", r.opts.ConfigFile)
		}
		if r.opts.Shell != "" {
			args = append(args, "--shell", r.opts.Shell)
		}
		var launchToken string
		if r.opts.Endpoint != "" && r.opts.Publish {
			args = append(args, "--endpoint", r.opts.Endpoint)
			if r.opts.AuthFile != "" {
				args = append(args, "--auth-file", r.opts.AuthFile)
			} else {
				launchToken = r.opts.Token
				if local.publisher != nil {
					launchToken = local.publisher.AccessToken()
				}
			}
			if r.opts.Insecure {
				args = append(args, "--insecure")
			}
		} else {
			args = append(args, "--offline")
		}
		var env []string
		if r.opts.TLSDir != "" {
			env = append(env, "LINGON_SERVER_TLS_DIR="+r.opts.TLSDir)
		}
		_, err = headless.Launch(ctx, headless.LaunchRequest{Executable: r.opts.HeadlessExecutable, ConfigDir: cfg, SessionID: id, Args: args, Env: env, Token: launchToken})
		if err != nil {
			return sessionmanage.Result{Error: err.Error()}
		}
		return sessionmanage.Result{SessionID: id, Name: id}
	})
}

func managementRequestID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		panic(err)
	}
	return fmt.Sprintf("%x", id[:])
}

func (m *remoteManager) Manage(ctx context.Context, req sessionmanage.Request) sessionmanage.Result {
	m.mu.Lock()
	view := m.views[req.SessionID]
	m.mu.Unlock()
	if view == nil || view.client == nil {
		return sessionmanage.Result{Error: "session is not connected"}
	}
	result, err := view.client.Manage(ctx, req)
	if err != nil {
		result.Error = err.Error()
	}
	if err == nil {
		_ = m.Refresh(ctx)
	}
	return result
}
