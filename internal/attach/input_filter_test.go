package attach

import "testing"

var filteredByteSink byte

func TestMouseReportFilterRemovesSGRMouseSequences(t *testing.T) {
	var f mouseReportFilter
	in := []byte("echo A\x1b[<0;26;16M\x1b[<0;26;16mecho B\n")
	got := string(f.Filter(in))
	want := "echo Aecho B\n"
	if got != want {
		t.Fatalf("filtered input = %q, want %q", got, want)
	}
}

func TestMouseReportFilterHandlesChunkBoundaries(t *testing.T) {
	var f mouseReportFilter
	part1 := f.Filter([]byte("echo A\x1b[<0;26;"))
	part2 := f.Filter([]byte("16Mecho B\n"))
	got := string(append(part1, part2...))
	want := "echo Aecho B\n"
	if got != want {
		t.Fatalf("filtered chunked input = %q, want %q", got, want)
	}
}

func TestFilterMouseByteNoPerByteAllocsInHotPath(t *testing.T) {
	checkNoAllocs := func(t *testing.T, path string) {
		t.Helper()
		var f mouseReportFilter
		out := make([]byte, 0, 8)
		allocs := testing.AllocsPerRun(100, func() {
			for i := 0; i < 4096; i++ {
				out = filterMouseByte(&f, byte('a'+(i%26)), out[:0])
				if len(out) > 0 {
					filteredByteSink ^= out[0]
				}
			}
		})
		if allocs != 0 {
			t.Fatalf("%s allocs/run = %f, want 0", path, allocs)
		}
	}

	t.Run("single-attach", func(t *testing.T) {
		checkNoAllocs(t, "single-attach")
	})
	t.Run("multi-attach", func(t *testing.T) {
		checkNoAllocs(t, "multi-attach")
	})
}
