package session

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

func shellFromPasswd(uid string) (string, error) {
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return "", err
	}
	defer func() {
		_ = f.Close()
	}()
	return shellFromPasswdReader(f, uid)
}

func shellFromPasswdReader(r io.Reader, uid string) (string, error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) < 7 {
			continue
		}
		if parts[2] == uid {
			return parts[6], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("user not found in /etc/passwd")
}
