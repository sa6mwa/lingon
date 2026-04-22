package cliwall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/authstore"
	"pkt.systems/prettyx"
)

// Request carries the effective wall command inputs, including which flags
// were explicitly set by the caller.
type Request struct {
	Loader *lingon.Loader

	Endpoint           string
	EndpointChanged    bool
	AccessToken        string
	AccessTokenChanged bool
	AuthFile           string
	AuthFileChanged    bool

	Message  string
	Insecure bool
	Quiet    bool
	Stdout   io.Writer
}

// Execute runs the Lingon wall command behavior in-process.
func Execute(ctx context.Context, req Request) error {
	if req.Loader == nil {
		return fmt.Errorf("loader is required")
	}
	cfg, err := req.Loader.Load()
	if err != nil {
		return err
	}
	tlsDir := cfg.Server.TLS.Dir

	authPath := strings.TrimSpace(req.AuthFile)
	if !req.AuthFileChanged {
		authPath = strings.TrimSpace(cfg.Client.AuthFile)
	}

	endpointValue, err := resolveEndpointValue(req.Loader, strings.TrimSpace(cfg.Client.Endpoint), strings.TrimSpace(req.Endpoint), authPath, req.EndpointChanged, req.AccessTokenChanged)
	if err != nil {
		return err
	}
	if endpointValue == "" {
		return fmt.Errorf("endpoint is required")
	}

	tokenValue := strings.TrimSpace(req.AccessToken)
	if !req.AccessTokenChanged {
		tokenValue, err = resolveAccessToken(ctx, endpointValue, authPath, tlsDir, req.Insecure)
		if err != nil {
			return err
		}
	}
	if tokenValue == "" {
		return fmt.Errorf("access token is required")
	}

	resp, err := lingon.Wall(ctx, lingon.WallOptions{
		Endpoint:    endpointValue,
		AccessToken: tokenValue,
		Message:     strings.TrimSpace(req.Message),
		TLSDir:      tlsDir,
		Insecure:    req.Insecure,
	})
	if err != nil {
		return err
	}
	if req.Quiet {
		return nil
	}
	return printJSON(outputWriter(req.Stdout), resp)
}

func outputWriter(w io.Writer) io.Writer {
	if w == nil {
		return io.Discard
	}
	return w
}

func resolveEndpointValue(loader *lingon.Loader, configuredEndpoint, flagEndpoint, authPath string, endpointChanged, accessTokenChanged bool) (string, error) {
	endpointValue := resolveConfiguredEndpointValue(loader, configuredEndpoint, flagEndpoint, endpointChanged)
	if endpointChanged {
		return endpointValue, nil
	}
	if endpointExplicitlyConfigured(loader) && strings.TrimSpace(configuredEndpoint) != "" {
		return endpointValue, nil
	}
	if accessTokenChanged {
		return endpointValue, nil
	}
	inferredEndpoint, err := inferEndpointFromAuth(authPath)
	if err != nil {
		return "", err
	}
	if inferredEndpoint != "" {
		return inferredEndpoint, nil
	}
	return endpointValue, nil
}

func resolveConfiguredEndpointValue(loader *lingon.Loader, configuredEndpoint, flagEndpoint string, endpointChanged bool) string {
	if endpointChanged {
		return strings.TrimSpace(flagEndpoint)
	}
	configuredEndpoint = strings.TrimSpace(configuredEndpoint)
	if endpointExplicitlyConfigured(loader) && configuredEndpoint != "" {
		return configuredEndpoint
	}
	if configuredEndpoint != "" {
		return configuredEndpoint
	}
	return lingon.DefaultClientEndpoint
}

func endpointExplicitlyConfigured(loader *lingon.Loader) bool {
	if value, ok := os.LookupEnv("LINGON_CLIENT_ENDPOINT"); ok && strings.TrimSpace(value) != "" {
		return true
	}
	if loader == nil {
		return false
	}
	return loader.Viper().InConfig("client.endpoint")
}

func inferEndpointFromAuth(authPath string) (string, error) {
	authPath = strings.TrimSpace(authPath)
	if authPath == "" {
		return "", nil
	}
	endpoints, err := authstore.Endpoints(authPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	switch len(endpoints) {
	case 0:
		return "", nil
	case 1:
		return endpoints[0], nil
	default:
		return "", fmt.Errorf("endpoint is ambiguous; pass --endpoint or set client.endpoint (stored endpoints: %s)", strings.Join(endpoints, ", "))
	}
}

func resolveAccessToken(ctx context.Context, endpoint, authPath, tlsDir string, insecure bool) (string, error) {
	state, err := lingon.EnsureAccessTokenWithTLSDirInsecure(ctx, endpoint, authPath, tlsDir, insecure)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("auth file not found at %s; run `lingon login -e %s`", authPath, endpoint)
		}
		return "", fmt.Errorf("%s; run `lingon login -e %s`", err.Error(), endpoint)
	}
	if state.AccessToken == "" {
		return "", fmt.Errorf("access token missing; run `lingon login -e %s`", endpoint)
	}
	return state.AccessToken, nil
}

func printJSON(w io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return prettyx.PrettyTo(w, data, prettyx.DefaultOptions)
}
