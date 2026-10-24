import systems.pkt.lingon.share.ShareTokens;

// A focused reproduction gate, separate from virtual-time lifecycle tests.
class ShareScanProbe {
    public static void main(String[] args) {
        for (int count : new int[]{20, 100, 500, 1000}) {
            String input = "LGE".repeat(count);
            long started = System.nanoTime();
            var parsed = ShareTokens.INSTANCE.findInText(input);
            long elapsed = System.nanoTime() - started;
            System.out.printf("chars=%d parsed=%s milliseconds=%.3f%n", input.length(), parsed, elapsed / 1e6);
            if (parsed != null) throw new AssertionError("malformed token accepted");
            if (count == 1000 && elapsed > 2_000_000_000L) throw new AssertionError("3KB reproduction exceeded two-second diagnostic bound");
        }
    }
}
