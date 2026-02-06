package main

import (
	"encoding/json"
	"io"

	"pkt.systems/prettyx"
)

const (
	ansiItalic = "\x1b[3m"
	ansiGray   = "\x1b[90m"
	ansiReset  = "\x1b[0m"
)

func formatItalicGray(message string) string {
	return ansiItalic + ansiGray + message + ansiReset
}

func printJSON(w io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return prettyx.PrettyTo(w, data, prettyx.DefaultOptions)
}
