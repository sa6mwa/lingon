package lifecycle

import "testing"

func TestProofExercisesBuildAndInstall(t *testing.T) {
	for _, g := range proofGates() {
		if g.tool == "bash" && len(g.args) == 1 && g.args[0] == "scripts/check-install.sh" {
			return
		}
	}
	t.Fatal("release proof does not exercise the build/install cycle")
}
