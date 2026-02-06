package systems.pkt.lingon.terminal

import kotlin.math.max
import systems.pkt.lingon.protocol.ScrollbackRow

fun buildScrollbackSnapshot(
    live: TerminalSnapshot,
    scrollback: List<ScrollbackRow>,
    offset: Int,
): TerminalSnapshot {
    if (offset <= 0 || scrollback.isEmpty()) {
        return live
    }
    val cols = live.cols
    val rows = live.rows
    if (cols <= 0 || rows <= 0) {
        return live
    }
    val totalRows = scrollback.size + rows
    val maxOffset = max(0, totalRows - rows)
    val clampedOffset = offset.coerceIn(0, maxOffset)
    if (clampedOffset == 0) {
        return live
    }
    var start = totalRows - rows - clampedOffset
    if (start < 0) {
        start = 0
    }

    val size = cols * rows
    val runes = IntArray(size)
    val modes = IntArray(size)
    val fg = IntArray(size)
    val bg = IntArray(size)
    val graphemes = if (live.graphemes != null) Array(size) { "" } else null
    var hasGraphemes = false

    fun fillFromRow(dstRow: Int, row: ScrollbackRow) {
        val base = dstRow * cols
        val rowRunes = row.runesList
        val rowModes = row.modesList
        val rowFg = row.fgList
        val rowBg = row.bgList
        val rowGraphemes = row.graphemesList
        for (x in 0 until cols) {
            val idx = base + x
            if (x < rowRunes.size) runes[idx] = rowRunes[x]
            if (x < rowModes.size) modes[idx] = rowModes[x]
            if (x < rowFg.size) fg[idx] = rowFg[x]
            if (x < rowBg.size) bg[idx] = rowBg[x]
            if (graphemes != null && x < rowGraphemes.size) {
                graphemes[idx] = rowGraphemes[x]
                if (rowGraphemes[x].isNotEmpty()) {
                    hasGraphemes = true
                }
            }
        }
    }

    for (viewRow in 0 until rows) {
        val sourceRow = start + viewRow
        if (sourceRow < scrollback.size) {
            fillFromRow(viewRow, scrollback[sourceRow])
            continue
        }
        val liveRow = sourceRow - scrollback.size
        if (liveRow < 0 || liveRow >= rows) {
            continue
        }
        val srcBase = liveRow * cols
        val dstBase = viewRow * cols
        for (x in 0 until cols) {
            val src = srcBase + x
            val dst = dstBase + x
            if (src < live.runes.size) runes[dst] = live.runes[src]
            if (src < live.modes.size) modes[dst] = live.modes[src]
            if (src < live.fg.size) fg[dst] = live.fg[src]
            if (src < live.bg.size) bg[dst] = live.bg[src]
            if (graphemes != null && live.graphemes != null && src < live.graphemes.size) {
                graphemes[dst] = live.graphemes[src]
                if (live.graphemes[src].isNotEmpty()) {
                    hasGraphemes = true
                }
            }
        }
    }

    val finalGraphemes = if (hasGraphemes) graphemes else null

    return TerminalSnapshot(
        cols = cols,
        rows = rows,
        runes = runes,
        modes = modes,
        fg = fg,
        bg = bg,
        graphemes = finalGraphemes,
        cursorX = 0,
        cursorY = 0,
        cursorVisible = false,
        mode = live.mode,
        title = live.title,
    )
}
