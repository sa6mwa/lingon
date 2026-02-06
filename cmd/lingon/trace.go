package main

import "pkt.systems/lingon/internal/trace"

func setupTrace(enabled bool, traceFile string) (*trace.Writer, string, error) {
	if traceFile == "" && !enabled {
		return nil, "", nil
	}
	path := traceFile
	if path == "" {
		defaultPath, err := trace.DefaultPath()
		if err != nil {
			return nil, "", err
		}
		path = defaultPath
	}
	writer, err := trace.New(path)
	if err != nil {
		return nil, "", err
	}
	return writer, path, nil
}
