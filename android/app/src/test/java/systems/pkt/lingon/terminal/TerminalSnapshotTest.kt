package systems.pkt.lingon.terminal

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertThrows
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

    @Test
    fun applyDiffClearsStaleGraphemeWhenPlainRuneUpdatesCell() {
        val snapshot = Snapshot.newBuilder()
            .setCols(1)
            .setRows(1)
            .addRunes(0)
            .addGraphemes("😀")
            .build()

        val model = TerminalSnapshot.fromProto(snapshot)
        val diff = Diff.newBuilder()
            .setCols(1)
            .setRows(1)
            .addDiffRows(
                DiffRow.newBuilder()
                    .setRow(0)
                    .addRunes(65)
                    .build(),
            )
            .build()

        val updated = TerminalSnapshot.applyDiff(model, diff)
        assertEquals(65, updated.runes[0])
        assertEquals("", updated.graphemes?.get(0))
    }

    @Test
    fun fromProtoRejectsOversizedDimensionsBeforeAllocatingCells() {
        val snapshot = Snapshot.newBuilder()
            .setCols(1000)
            .setRows(1000)
            .build()

        assertThrows(IllegalArgumentException::class.java) {
            TerminalSnapshot.fromProto(snapshot)
        }
    }

    @Test
    fun applyDiffRejectsOversizedDimensionsBeforeAllocatingCells() {
        val diff = Diff.newBuilder()
            .setCols(1000)
            .setRows(1000)
            .build()

        assertThrows(IllegalArgumentException::class.java) {
            TerminalSnapshot.applyDiff(null, diff)
        }
    }
}
