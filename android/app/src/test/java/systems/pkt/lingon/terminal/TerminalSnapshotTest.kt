package systems.pkt.lingon.terminal

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Test
import systems.pkt.lingon.protocol.Diff
import systems.pkt.lingon.protocol.DiffRow
import systems.pkt.lingon.protocol.Snapshot

class TerminalSnapshotTest {
    @Test
    fun applyDiffUpdatesCells() {
        val snapshot = Snapshot.newBuilder()
            .setCols(2)
            .setRows(1)
            .addAllRunes(listOf(65, 66))
            .addAllModes(listOf(0, 0))
            .addAllFg(listOf(0, 0))
            .addAllBg(listOf(0, 0))
            .build()

        val model = TerminalSnapshot.fromProto(snapshot)
        val diff = Diff.newBuilder()
            .setCols(2)
            .setRows(1)
            .addDiffRows(
                DiffRow.newBuilder()
                    .setRow(0)
                    .addAllRunes(listOf(67))
                    .build(),
            )
            .build()

        val updated = TerminalSnapshot.applyDiff(model, diff)
        assertEquals(67, updated.runes[0])
        assertEquals(66, updated.runes[1])
    }

    @Test
    fun applyDiffAllocatesGraphemes() {
        val snapshot = Snapshot.newBuilder()
            .setCols(2)
            .setRows(1)
            .addAllRunes(listOf(0, 0))
            .addAllModes(listOf(0, 0))
            .addAllFg(listOf(0, 0))
            .addAllBg(listOf(0, 0))
            .build()

        val model = TerminalSnapshot.fromProto(snapshot)
        val diff = Diff.newBuilder()
            .setCols(2)
            .setRows(1)
            .addDiffRows(
                DiffRow.newBuilder()
                    .setRow(0)
                    .addAllGraphemes(listOf("ZZ", ""))
                    .build(),
            )
            .build()

        val updated = TerminalSnapshot.applyDiff(model, diff)
        assertNotNull(updated.graphemes)
        assertEquals("ZZ", updated.graphemes?.get(0))
    }
}
