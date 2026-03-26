package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/authstore"
)

func resolveEndpointValue(cmd *cobra.Command, loader *lingon.Loader, configuredEndpoint, flagEndpoint, authPath string) (string, error) {
	if cmd != nil && cmd.Flags().Changed("endpoint") {
		return strings.TrimSpace(flagEndpoint), nil
	}

	configuredEndpoint = strings.TrimSpace(configuredEndpoint)
	if endpointExplicitlyConfigured(loader) && configuredEndpoint != "" {
		return configuredEndpoint, nil
	}

	if authEndpointInferenceEnabled(cmd) {
		inferredEndpoint, err := inferEndpointFromAuth(authPath)
		if err != nil {
			return "", err
		}
		if inferredEndpoint != "" {
			return inferredEndpoint, nil
		}
	}

	if configuredEndpoint != "" {
		return configuredEndpoint, nil
	}
	return lingon.DefaultClientEndpoint, nil
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

func endpointExplicitlyConfigured(loader *lingon.Loader) bool {
	if value, ok := os.LookupEnv("LINGON_CLIENT_ENDPOINT"); ok && strings.TrimSpace(value) != "" {
		return true
	}
	if loader == nil {
		return false
	}
	return loader.Viper().InConfig("client.endpoint")
}

func authEndpointInferenceEnabled(cmd *cobra.Command) bool {
	if cmd == nil {
		return true
	}
	if cmd.Name() == "login" {
		return false
	}
	return !flagChanged(cmd, "token") && !flagChanged(cmd, "access-token")
}

func flagChanged(cmd *cobra.Command, name string) bool {
	if cmd == nil {
		return false
	}
	flag := cmd.Flags().Lookup(name)
	return flag != nil && flag.Changed
}
