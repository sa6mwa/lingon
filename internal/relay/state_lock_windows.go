//go:build windows

package relay

import (
	"fmt"
	"os"
)

func acquireStateFile(string) (*os.File, error) {
	return nil, fmt.Errorf("local relay administration requires Unix file ownership")
}

func directoryIdentity(string) (uint64, uint64, error) {
	return 0, 0, fmt.Errorf("local relay administration requires Unix file identity")
}
