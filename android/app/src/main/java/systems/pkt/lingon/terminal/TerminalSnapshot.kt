package systems.pkt.lingon.terminal

import systems.pkt.lingon.protocol.Cursor
import systems.pkt.lingon.protocol.Diff
import systems.pkt.lingon.protocol.DiffRow
import systems.pkt.lingon.protocol.Snapshot

private const val MaxTerminalCols = 1000
private const val MaxTerminalRows = 1000
private const val MaxTerminalCells = 250_000

class TerminalSnapshot(
    val cols: Int,
    val rows: Int,
    val runes: IntArray,
    val modes: IntArray,
    val fg: IntArray,
    val bg: IntArray,
    val graphemes: Array<String>?,
    var cursorX: Int,
    var cursorY: Int,
    var cursorVisible: Boolean,
    var mode: Int,
    var title: String,
) {
    companion object {
        fun fromProto(snapshot: Snapshot): TerminalSnapshot {
            val cols = snapshot.cols
            val rows = snapshot.rows
            val cellCount = checkedCellCount(cols, rows)
            val runes = IntArray(cellCount)
            val modes = IntArray(cellCount)
            val fg = IntArray(cellCount)
            val bg = IntArray(cellCount)
            val graphemes = if (snapshot.graphemesCount > 0) Array(cellCount) { "" } else null

            val runesList = snapshot.runesList
            val modesList = snapshot.modesList
            val fgList = snapshot.fgList
            val bgList = snapshot.bgList
            val graphemeList = snapshot.graphemesList

            for (i in 0 until cellCount) {
                if (i < runesList.size) runes[i] = runesList[i]
                if (i < modesList.size) modes[i] = modesList[i]
                if (i < fgList.size) fg[i] = fgList[i]
                if (i < bgList.size) bg[i] = bgList[i]
                if (graphemes != null && i < graphemeList.size) {
                    graphemes[i] = graphemeList[i]
                }
            }

            val cursor = snapshot.cursor ?: Cursor.getDefaultInstance()
            return TerminalSnapshot(
                cols = cols,
                rows = rows,
                runes = runes,
                modes = modes,
                fg = fg,
                bg = bg,
                graphemes = graphemes,
                cursorX = cursor.x,
                cursorY = cursor.y,
                cursorVisible = snapshot.cursorVisible,
                mode = snapshot.mode,
                title = snapshot.title,
            )
        }

        fun applyDiff(snapshot: TerminalSnapshot?, diff: Diff): TerminalSnapshot {
            val cols = if (diff.cols > 0) diff.cols else snapshot?.cols ?: 0
            val rows = if (diff.rows > 0) diff.rows else snapshot?.rows ?: 0
            val cellCount = checkedCellCount(cols, rows)
            val hasGraphemes = diff.diffRowsList.any { it.graphemesCount > 0 }

            val current = if (snapshot == null || snapshot.cols != cols || snapshot.rows != rows) {
                TerminalSnapshot(
                    cols = cols,
                    rows = rows,
                    runes = IntArray(cellCount),
                    modes = IntArray(cellCount),
                    fg = IntArray(cellCount),
                    bg = IntArray(cellCount),
                    graphemes = if (hasGraphemes) Array(cellCount) { "" } else null,
                    cursorX = 0,
                    cursorY = 0,
                    cursorVisible = false,
                    mode = 0,
                    title = "",
                )
            } else {
                if (snapshot.graphemes == null && hasGraphemes) {
                    val expanded = Array(cellCount) { "" }
                    TerminalSnapshot(
                        cols = snapshot.cols,
                        rows = snapshot.rows,
                        runes = snapshot.runes,
                        modes = snapshot.modes,
                        fg = snapshot.fg,
                        bg = snapshot.bg,
                        graphemes = expanded,
                        cursorX = snapshot.cursorX,
                        cursorY = snapshot.cursorY,
                        cursorVisible = snapshot.cursorVisible,
                        mode = snapshot.mode,
                        title = snapshot.title,
                    )
                } else {
                    snapshot
                }
            }

            for (row in diff.diffRowsList) {
                applyRow(current, row)
            }

            val cursor = diff.cursor ?: Cursor.getDefaultInstance()
            current.cursorX = cursor.x
            current.cursorY = cursor.y
            current.cursorVisible = diff.cursorVisible
            current.mode = diff.mode
            current.title = diff.title
            return current
        }

        private fun checkedCellCount(cols: Int, rows: Int): Int {
            if (cols <= 0 || rows <= 0 || cols > MaxTerminalCols || rows > MaxTerminalRows) {
                throw IllegalArgumentException("invalid terminal size ${cols}x${rows}")
            }
            val cells = cols.toLong() * rows.toLong()
            if (cells > MaxTerminalCells) {
                throw IllegalArgumentException("terminal size ${cols}x${rows} exceeds supported cell limit")
            }
            return cells.toInt()
        }

        private fun applyRow(snapshot: TerminalSnapshot, row: DiffRow) {
            val y = row.row
            if (y < 0 || y >= snapshot.rows) return
            val start = y * snapshot.cols
            val runesList = row.runesList
            val modesList = row.modesList
            val fgList = row.fgList
            val bgList = row.bgList
            val graphemeList = row.graphemesList
            for (x in 0 until snapshot.cols) {
                val idx = start + x
                if (x < runesList.size) snapshot.runes[idx] = runesList[x]
                if (x < modesList.size) snapshot.modes[idx] = modesList[x]
                if (x < fgList.size) snapshot.fg[idx] = fgList[x]
                if (x < bgList.size) snapshot.bg[idx] = bgList[x]
                if (snapshot.graphemes != null && x < graphemeList.size) {
                    snapshot.graphemes[idx] = graphemeList[x]
                } else if (snapshot.graphemes != null && x < runesList.size) {
                    snapshot.graphemes[idx] = ""
                }
            }
        }
    }
}
