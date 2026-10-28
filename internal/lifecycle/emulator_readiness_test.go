package lifecycle

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
)

func TestEmulatorReadinessRequiresUnlockedStorage(t *testing.T) {
	for _, state := range []string{"ready", "booting", "locked", "adb-failed", "boot-completes", "unlocks"} {
		t.Run(state, func(t *testing.T) {
			dir := t.TempDir()
			log := filepath.Join(dir, "adb.log")
			// Generate the shell's clock from the injected mock. Two attempts
			// fit before expiry; sleep below never waits on wall time.
			mock := clock.NewMock()
			start := mock.Now().Unix()
			mock.Add(3 * time.Second)
			second := mock.Now().Unix()
			mock.Add(3 * time.Second)
			expired := mock.Now().Unix()
			date := fmt.Sprintf(`#!/bin/bash
n=0
[[ ! -f "$0.count" ]] || read -r n < "$0.count"
echo "$((n+1))" > "$0.count"
if (( n < 2 )); then echo %d; elif (( n == 2 )); then echo %d; else echo %d; fi
`, start, second, expired)
			adb := `#!/bin/bash
printf '%s\n' "$*" >> "$READINESS_LOG"
[[ "$1" != -s ]] || shift 2
case "$*" in
  wait-for-device) [[ "$READINESS_STATE" != adb-failed ]] ;;
  'shell getprop sys.boot_completed')
    if [[ "$READINESS_STATE" == booting ]]; then echo 0
    elif [[ "$READINESS_STATE" == boot-completes && ! -f "$0.boot" ]]; then touch "$0.boot"; echo 0
    else echo 1; fi ;;
  'shell am get-started-user-state 0')
    if [[ "$READINESS_STATE" == locked ]]; then echo RUNNING_LOCKED
    elif [[ "$READINESS_STATE" == unlocks && ! -f "$0.unlocked" ]]; then touch "$0.unlocked"; echo RUNNING_LOCKED
    else echo RUNNING_UNLOCKED; fi ;;
  'shell getprop ro.kernel.qemu') echo 1 ;;
  *) exit 0 ;;
esac
`
			for name, source := range map[string]string{"date": date, "adb": adb, "sleep": "#!/bin/sh\nexit 0\n"} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0700); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "bash", "../../android/scripts/ensure-emu-keyboard.sh")
			cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "ADB="+filepath.Join(dir, "adb"), "ADB_SERIAL=emulator-owned", "BOOT_TIMEOUT_SECS=5", "AUTO_PUSH_CA_PEM=0", "READINESS_LOG="+log, "READINESS_STATE="+state)
			out, err := cmd.CombinedOutput()
			ready := state == "ready" || state == "boot-completes" || state == "unlocks"
			if ready && err != nil {
				t.Fatalf("ready emulator rejected: %v\n%s", err, out)
			}
			if !ready && err == nil {
				t.Fatalf("unready emulator (%s) accepted:\n%s", state, out)
			}
			calls, readErr := os.ReadFile(log)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !ready && strings.Contains(string(calls), " reverse ") {
				t.Fatalf("configured an unready emulator:\n%s", calls)
			}
			if ready && !strings.Contains(string(calls), "shell am get-started-user-state 0") {
				t.Fatal("readiness never checked credential-encrypted storage")
			}
		})
	}
}
