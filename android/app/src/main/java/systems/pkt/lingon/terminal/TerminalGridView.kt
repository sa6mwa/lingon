package systems.pkt.lingon.terminal

import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.text.TextPaint
import android.util.AttributeSet
import android.util.TypedValue
import android.view.GestureDetector
import android.view.MotionEvent
import android.view.ScaleGestureDetector
import android.view.View
import androidx.core.content.res.ResourcesCompat
import androidx.compose.ui.graphics.toArgb
import systems.pkt.lingon.R
import systems.pkt.lingon.DefaultTerminalZoom
import systems.pkt.lingon.MaxTerminalRenderFontSizeSp
import systems.pkt.lingon.MaxTerminalZoom
import systems.pkt.lingon.MinTerminalFontSizeSp
import systems.pkt.lingon.MinTerminalZoom
import kotlin.math.abs
import kotlin.math.ceil
import kotlin.math.floor
import kotlin.math.max
import kotlin.math.min

class TerminalGridView @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
) : View(context, attrs) {
    private var snapshot: TerminalSnapshot? = null
    private var palette: TerminalPalette? = null
    private var baseFontSizeSp: Int = 0
    private var minFontSizeSp: Int = MinTerminalFontSizeSp
    private var frameSeq: Long = Long.MIN_VALUE
    private var hostCols: Int = 0
    private var hostRows: Int = 0
    private var fitToViewWidth: Boolean = true
    private var viewCols: Int = 0
    private var viewRows: Int = 0
    private var zoomFactor: Float = DefaultTerminalZoom
    private var panResetNonce: Int = 0
    private var imeVisible: Boolean = false
    private var isLoading: Boolean = false
    private var scrollbackOffsetRows: Int = 0
    private var cameraOffsetXPx: Float = 0f
    private var cameraOffsetYPx: Float = 0f
    private var pendingViewportState: TerminalViewportState? = null
    private var lastViewportHeightPx: Int = 0
    private var panActive: Boolean = false
    private var lastTouchX: Float = 0f
    private var lastTouchY: Float = 0f
    private var onViewSizeChanged: ((Int, Int) -> Unit)? = null
    private var onZoomChanged: ((Float) -> Unit)? = null
    private var onTap: (() -> Unit)? = null
    private var onScrollback: ((Int) -> Unit)? = null
    private var scrollRemainderY: Float = 0f
    private var pendingLiveReentryRows: Int = 0
    private var pendingScrollbackEntryRows: Int = 0
    private var pendingScrollbackEntryYPx: Float = 0f
    private var lastCursorX: Int = Int.MIN_VALUE
    private var lastCursorY: Int = Int.MIN_VALUE
    private var cursorFollowAfterInput: Boolean = false
    private var suppressCursorFollowForScrollbackReentry: Boolean = false

    private val gestureDetector = GestureDetector(context, object : GestureDetector.SimpleOnGestureListener() {
        override fun onDown(e: MotionEvent): Boolean {
            return true
        }

        override fun onSingleTapUp(e: MotionEvent): Boolean {
            performClick()
            return true
        }

        override fun onScroll(
            e1: MotionEvent?,
            e2: MotionEvent,
            distanceX: Float,
            distanceY: Float,
        ): Boolean {
            if (scaleDetector.isInProgress || panActive) {
                return false
            }
            if (kotlin.math.abs(distanceY) < kotlin.math.abs(distanceX)) {
                return false
            }
            if (scaledCellHeight <= 0f) {
                return false
            }
            // Natural terminal panning: finger down reveals older lines.
            scrollRemainderY -= distanceY
            val deltaRows = (scrollRemainderY / scaledCellHeight).toInt()
            if (deltaRows != 0) {
                scrollRemainderY -= deltaRows * scaledCellHeight
                onScrollback?.invoke(deltaRows)
            }
            return true
        }
    })

    private val scaleDetector = ScaleGestureDetector(context, object : ScaleGestureDetector.SimpleOnScaleGestureListener() {
        override fun onScaleBegin(detector: ScaleGestureDetector): Boolean {
            panActive = false
            cursorFollowAfterInput = false
            return true
        }

        override fun onScale(detector: ScaleGestureDetector): Boolean {
            val current = zoomFactor
            val normalized = normalizeZoom(current * detector.scaleFactor)
            if (abs(normalized - current) > zoomEpsilon) {
                val oldScaledCellWidth = scaledCellWidth
                val oldScaledCellHeight = scaledCellHeight
                val focusCellX = if (oldScaledCellWidth > 0f) {
                    (cameraOffsetXPx + detector.focusX) / oldScaledCellWidth
                } else {
                    0f
                }
                val focusCellY = if (oldScaledCellHeight > 0f) {
                    (cameraOffsetYPx + detector.focusY) / oldScaledCellHeight
                } else {
                    0f
                }

                zoomFactor = normalized
                updateLayout()
                if (scaledCellWidth > 0f) {
                    cameraOffsetXPx = focusCellX * scaledCellWidth - detector.focusX
                }
                if (scaledCellHeight > 0f) {
                    cameraOffsetYPx = focusCellY * scaledCellHeight - detector.focusY
                }
                clampCameraOffsets(resetScrollRemainder = true)
                invalidate()
                onZoomChanged?.invoke(normalized)
            }
            return true
        }
    })

    private val bgPaint = Paint(Paint.ANTI_ALIAS_FLAG)
    private val linePaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        strokeWidth = 1f
        style = Paint.Style.STROKE
    }

    private var paintNormal: TextPaint = TextPaint(Paint.ANTI_ALIAS_FLAG)
    private var paintBold: TextPaint = TextPaint(Paint.ANTI_ALIAS_FLAG)
    private var paintItalic: TextPaint = TextPaint(Paint.ANTI_ALIAS_FLAG)
    private var paintBoldItalic: TextPaint = TextPaint(Paint.ANTI_ALIAS_FLAG)

    private var cellWidth = 0f
    private var cellHeight = 0f
    private var baseline = 0f
    private var scaledCellWidth = 0f
    private var scaledCellHeight = 0f
    private var renderScaleX = 1f
    private var renderScaleY = 1f

    init {
        isFocusable = false
        isFocusableInTouchMode = false
        isClickable = true
    }

    fun update(
        snapshot: TerminalSnapshot?,
        fontSizeSp: Int,
        minFontSizeSp: Int,
        palette: TerminalPalette,
        frameSeq: Long,
        hostCols: Int,
        hostRows: Int,
        fitToViewWidth: Boolean,
        zoomFactor: Float,
        panResetNonce: Int,
        scrollbackOffsetRows: Int,
        imeVisible: Boolean,
        isLoading: Boolean,
    ) {
        var invalidate = false
        if (this.snapshot !== snapshot) {
            this.snapshot = snapshot
            invalidate = true
        }
        if (this.palette !== palette) {
            this.palette = palette
            invalidate = true
        }
        if (this.baseFontSizeSp != fontSizeSp) {
            this.baseFontSizeSp = fontSizeSp
            invalidate = true
        }
        if (this.minFontSizeSp != minFontSizeSp) {
            this.minFontSizeSp = minFontSizeSp
            invalidate = true
        }
        if (this.frameSeq != frameSeq) {
            this.frameSeq = frameSeq
            invalidate = true
        }
        if (this.hostCols != hostCols) {
            this.hostCols = hostCols
            invalidate = true
        }
        if (this.hostRows != hostRows) {
            this.hostRows = hostRows
            invalidate = true
        }
        if (this.fitToViewWidth != fitToViewWidth) {
            this.fitToViewWidth = fitToViewWidth
            invalidate = true
        }
        if (this.zoomFactor != zoomFactor) {
            this.zoomFactor = zoomFactor
            invalidate = true
        }
        if (panResetNonce > this.panResetNonce) {
            this.panResetNonce = panResetNonce
            resetPan()
            invalidate = true
        } else if (this.panResetNonce != panResetNonce) {
            this.panResetNonce = panResetNonce
        }
        if (this.scrollbackOffsetRows != scrollbackOffsetRows) {
            val nextScrollbackOffsetRows = scrollbackOffsetRows.coerceAtLeast(0)
            applyScrollbackOffsetChange(previousOffsetRows = this.scrollbackOffsetRows, nextOffsetRows = nextScrollbackOffsetRows)
            this.scrollbackOffsetRows = nextScrollbackOffsetRows
            invalidate = true
        }
        if (this.imeVisible != imeVisible) {
            this.imeVisible = imeVisible
            invalidate = true
        }
        if (this.isLoading != isLoading) {
            this.isLoading = isLoading
            invalidate = true
        }
        if (invalidate) {
            updateLayout()
            applyPendingViewportRestoreIfReady()
            invalidate()
        }
    }

    fun setOnViewSizeChanged(listener: ((Int, Int) -> Unit)?) {
        onViewSizeChanged = listener
    }

    fun setOnZoomChanged(listener: ((Float) -> Unit)?) {
        onZoomChanged = listener
    }

    fun setOnTap(listener: (() -> Unit)?) {
        onTap = listener
    }

    fun setOnScrollback(listener: ((Int) -> Unit)?) {
        onScrollback = listener
    }

    fun getViewCols(): Int = viewCols

    fun getViewRows(): Int = viewRows

    fun getRenderScaleX(): Float = renderScaleX

    fun getRenderScaleY(): Float = renderScaleY

    fun getCameraOffsetYForTesting(): Float = cameraOffsetYPx

    fun getScaledCellHeightForTesting(): Float = scaledCellHeight

    fun getVisibleStartRow(): Int {
        val snap = snapshot ?: return 0
        val rows = snap.rows
        if (rows <= 0 || scaledCellHeight <= 0f || height <= 0) return 0
        val maxRows = floor(height / scaledCellHeight).toInt().coerceAtLeast(0)
        val visibleRows = if (maxRows <= 0) rows else minOf(rows, maxRows)
        val maxOffsetRows = max(0, rows - visibleRows)
        val startRow = floor(cameraOffsetYPx.coerceAtLeast(0f) / scaledCellHeight).toInt()
        return startRow.coerceIn(0, maxOffsetRows)
    }

    fun getVisibleEndRowExclusive(): Int {
        val snap = snapshot ?: return 0
        val rows = snap.rows
        if (rows <= 0 || scaledCellHeight <= 0f || height <= 0) return 0
        val maxRows = floor(height / scaledCellHeight).toInt().coerceAtLeast(0)
        val visibleRows = if (maxRows <= 0) rows else minOf(rows, maxRows)
        return (getVisibleStartRow() + visibleRows).coerceAtMost(rows)
    }

    fun captureViewportState(): TerminalViewportState {
        return TerminalViewportState(
            cameraOffsetXPx = cameraOffsetXPx,
            cameraOffsetYPx = cameraOffsetYPx,
            scrollRemainderY = scrollRemainderY,
            viewportHeightPx = height,
        )
    }

    fun scheduleViewportRestore(state: TerminalViewportState?) {
        pendingViewportState = state
        applyPendingViewportRestoreIfReady()
    }

    fun restoreViewportState(state: TerminalViewportState?) {
        if (state == null) return
        pendingViewportState = null
        applyViewportRestore(state)
    }

    private fun applyPendingViewportRestoreIfReady() {
        val state = pendingViewportState ?: return
        if (height <= 0 || scaledCellHeight <= 0f) return
        pendingViewportState = null
        applyViewportRestore(state)
    }

    private fun applyViewportRestore(state: TerminalViewportState) {
        cameraOffsetXPx = state.cameraOffsetXPx
        val totalRows = snapshot?.rows ?: 0
        cameraOffsetYPx = if (TerminalViewportPolicy.shouldSnapToLiveBottom(
                zoomFactor = zoomFactor,
                scrollbackOffsetRows = scrollbackOffsetRows,
            )
        ) {
            val snap = snapshot
            if (snap != null && snap.cursorVisible) {
                TerminalViewportPolicy.autoFollowCursorCameraOffsetY(
                    cameraOffsetYPx = state.cameraOffsetYPx,
                    scaledCellHeightPx = scaledCellHeight,
                    viewportHeightPx = height,
                    totalRows = totalRows,
                    cursorY = snap.cursorY,
                )
            } else {
                TerminalViewportPolicy.bottomAlignedCameraOffsetY(
                    totalRows = totalRows,
                    scaledCellHeightPx = scaledCellHeight,
                    viewportHeightPx = height,
                )
            }
        } else {
            TerminalViewportPolicy.preserveBottomAnchorOnHeightChange(
                cameraOffsetYPx = state.cameraOffsetYPx,
                previousViewportHeightPx = state.viewportHeightPx,
                nextViewportHeightPx = height,
                totalRows = totalRows,
                scaledCellHeightPx = scaledCellHeight,
            )
        }
        scrollRemainderY = state.scrollRemainderY
        clampCameraOffsets(resetScrollRemainder = false)
        invalidate()
    }

    override fun onSizeChanged(w: Int, h: Int, oldw: Int, oldh: Int) {
        super.onSizeChanged(w, h, oldw, oldh)
        updateLayout()
    }

    override fun performClick(): Boolean {
        super.performClick()
        onTap?.invoke()
        return true
    }

    override fun onTouchEvent(event: MotionEvent): Boolean {
        scaleDetector.onTouchEvent(event)
        gestureDetector.onTouchEvent(event)
        when (event.actionMasked) {
            MotionEvent.ACTION_DOWN -> {
                lastTouchX = event.x
                lastTouchY = event.y
                panActive = zoomFactor > DefaultTerminalZoom + zoomEpsilon
                if (panActive) {
                    cursorFollowAfterInput = false
                }
            }
            MotionEvent.ACTION_MOVE -> {
                if (panActive && !scaleDetector.isInProgress) {
                    val dx = event.x - lastTouchX
                    val dy = event.y - lastTouchY
                    applyPanDelta(dx, dy)
                    lastTouchX = event.x
                    lastTouchY = event.y
                }
            }
            MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> {
                panActive = false
            }
        }
        return true
    }

    override fun onDraw(canvas: Canvas) {
        super.onDraw(canvas)
        val snap = snapshot ?: return
        val pal = palette ?: return
        val cols = snap.cols
        val rows = snap.rows
        if (cols <= 0 || rows <= 0) return
        val cellW = cellWidth
        val cellH = cellHeight
        val scaledW = scaledCellWidth
        val scaledH = scaledCellHeight
        if (cellW <= 0f || cellH <= 0f || scaledW <= 0f || scaledH <= 0f) return

        val maxCols = floor(width / scaledW).toInt().coerceAtLeast(0)
        val maxRows = floor(height / scaledH).toInt().coerceAtLeast(0)
        val visibleCols = if (maxCols <= 0) cols else minOf(cols, maxCols)
        val visibleRows = if (maxRows <= 0) rows else minOf(rows, maxRows)
        val maxOffsetXPx = max(0f, (cols * scaledW) - width.toFloat())
        val maxOffsetYPx = max(0f, (rows * scaledH) - height.toFloat())
        if (isLoading || scrollbackOffsetRows > 0) {
            cursorFollowAfterInput = false
        }
        if (snap.cursorVisible) {
            val cursorX = snap.cursorX.coerceIn(0, cols - 1)
            val cursorY = snap.cursorY.coerceIn(0, rows - 1)
            val cursorMoved = cursorX != lastCursorX || cursorY != lastCursorY
            if (
                !isLoading &&
                cursorMoved &&
                scrollbackOffsetRows <= 0 &&
                !suppressCursorFollowForScrollbackReentry
            ) {
                cursorFollowAfterInput = true
            }
            suppressCursorFollowForScrollbackReentry = false
            lastCursorX = cursorX
            lastCursorY = cursorY
        } else {
            lastCursorX = Int.MIN_VALUE
            lastCursorY = Int.MIN_VALUE
            cursorFollowAfterInput = false
            suppressCursorFollowForScrollbackReentry = false
        }
        if (snap.cursorVisible && cursorFollowAfterInput) {
            val adjustedX = TerminalViewportPolicy.autoFollowCursorCameraOffsetX(
                panActive = panActive,
                scrollbackOffsetRows = scrollbackOffsetRows,
                cameraOffsetXPx = cameraOffsetXPx,
                scaledCellWidthPx = scaledW,
                viewportWidthPx = width,
                totalCols = cols,
                cursorX = snap.cursorX,
            )
            cameraOffsetXPx = adjustedX
        }
        val cameraX = cameraOffsetXPx.coerceIn(0f, maxOffsetXPx)
        val cameraY = cameraOffsetYPx.coerceIn(0f, maxOffsetYPx)
        if (cameraX != cameraOffsetXPx || cameraY != cameraOffsetYPx) {
            cameraOffsetXPx = cameraX
            cameraOffsetYPx = cameraY
        }
        val panOffsetCols = floor(cameraX / scaledW).toInt()
        val panOffsetRows = floor(cameraY / scaledH).toInt()
        val maxOffsetRows = max(0, rows - visibleRows)
        var followCameraYPx: Float? = null
        val startRow = if (isLoading) {
            panOffsetRows.coerceIn(0, maxOffsetRows)
        } else if (TerminalViewportPolicy.shouldAutoFollowCursor(
                zoomFactor = zoomFactor,
                panOffsetCols = panOffsetCols.coerceAtLeast(0),
                panOffsetRows = panOffsetRows.coerceAtLeast(0),
                totalRows = rows,
                visibleRows = visibleRows,
            )
        ) {
            val cursorY = if (snap.cursorVisible) snap.cursorY else rows - 1
            followCameraYPx = TerminalViewportPolicy.autoFollowCursorCameraOffsetY(
                cameraOffsetYPx = cameraY,
                scaledCellHeightPx = scaledH,
                viewportHeightPx = height,
                totalRows = rows,
                cursorY = cursorY,
            )
            floor(followCameraYPx / scaledH).toInt().coerceIn(0, maxOffsetRows)
        } else if (!isLoading && snap.cursorVisible && cursorFollowAfterInput) {
            val cursorY = snap.cursorY.coerceIn(0, rows - 1)
            followCameraYPx = TerminalViewportPolicy.autoFollowCursorCameraOffsetY(
                cameraOffsetYPx = cameraY,
                scaledCellHeightPx = scaledH,
                viewportHeightPx = height,
                totalRows = rows,
                cursorY = cursorY,
            )
            floor(followCameraYPx / scaledH).toInt().coerceIn(0, maxOffsetRows)
        } else {
            panOffsetRows.coerceIn(0, maxOffsetRows)
        }
        val startCol = floor(cameraX / scaledW).toInt().coerceIn(0, max(0, cols - 1))
        val effectiveOffsetX = cameraX
        val effectiveOffsetY = followCameraYPx ?: if (startRow != panOffsetRows.coerceIn(0, maxOffsetRows)) {
            startRow * scaledH
        } else {
            cameraY
        }
        if (effectiveOffsetY != cameraOffsetYPx) {
            cameraOffsetYPx = effectiveOffsetY
        }
        val fracX = effectiveOffsetX - (startCol * scaledW)
        val fracY = effectiveOffsetY - (startRow * scaledH)
        val renderCols = minOf(
            cols - startCol,
            ((width + fracX) / scaledW).toInt() + 2,
        ).coerceAtLeast(1)
        val renderRows = minOf(
            rows - startRow,
            ((height + fracY) / scaledH).toInt() + 2,
        ).coerceAtLeast(1)
        val endColExclusive = (startCol + renderCols).coerceAtMost(cols)
        val endRowExclusive = (startRow + renderRows).coerceAtMost(rows)

        bgPaint.color = pal.defaultBg.toArgb()
        canvas.drawRect(0f, 0f, width.toFloat(), height.toFloat(), bgPaint)

        canvas.save()
        // Scaled/translated rendering can produce negative origins; keep all paint strictly in-view.
        canvas.clipRect(0f, 0f, width.toFloat(), height.toFloat())
        canvas.scale(renderScaleX, renderScaleY)
        canvas.translate(
            -(startCol * cellW + (fracX / renderScaleX)),
            -(startRow * cellH + (fracY / renderScaleY)),
        )

        val underlineY = cellH - 2f
        var y = startRow
        while (y < endRowExclusive) {
            var x = startCol
            while (x < endColExclusive) {
                val idx = y * cols + x
                val attr = CellAttr(
                    mode = snap.modes.getOrElse(idx) { 0 },
                    fg = snap.fg.getOrElse(idx) { COLOR_DEFAULT },
                    bg = snap.bg.getOrElse(idx) { COLOR_DEFAULT },
                )
                val grapheme = snap.graphemes?.getOrElse(idx) { "" } ?: ""
                val rune = snap.runes.getOrElse(idx) { 32 }
                val width = cellWidth(grapheme)
                val skipNext = width > 1 && x + 1 < endColExclusive && isContinuationCell(snap, idx + 1, attr)

                val text = if (attr.mode and MODE_HIDDEN != 0) {
                    ""
                } else if (grapheme.isNotEmpty()) {
                    grapheme
                } else {
                    if (rune == 0) " " else String(Character.toChars(rune))
                }

                var fg = resolveColor(attr.fg, pal, isForeground = true)
                var bg = resolveColor(attr.bg, pal, isForeground = false)
                if (attr.mode and MODE_INVERSE != 0) {
                    val swapped = fg
                    fg = bg
                    bg = swapped
                }
                if (attr.mode and MODE_FAINT != 0) {
                    fg = applyFaint(fg)
                }

                val left = x * cellW
                val top = y * cellH
                bgPaint.color = bg.toArgb()
                canvas.drawRect(left, top, left + cellW, top + cellH, bgPaint)

                if (text.isNotEmpty()) {
                    val paint = resolvePaint(attr.mode)
                    paint.color = fg.toArgb()
                    canvas.drawText(text, left, top + baseline, paint)
                }

                if (attr.mode and MODE_UNDERLINE != 0) {
                    linePaint.color = fg.toArgb()
                    val yLine = top + underlineY
                    canvas.drawLine(left, yLine, left + cellW, yLine, linePaint)
                }

                x += if (skipNext) 2 else 1
            }
            y += 1
        }

        if (snap.cursorVisible) {
            val cx = snap.cursorX.coerceIn(0, cols - 1)
            val cy = snap.cursorY.coerceIn(0, rows - 1)
            val screenX = (cx * scaledW) - effectiveOffsetX
            val screenY = (cy * scaledH) - effectiveOffsetY
            if (screenX + scaledW > 0f && screenY + scaledH > 0f && screenX < width && screenY < height) {
                val left = cx * cellW
                val top = cy * cellH
                val fill = pal.cursor.copy(alpha = 0.35f).toArgb()
                bgPaint.color = fill
                canvas.drawRect(left, top, left + cellW, top + cellH, bgPaint)
                linePaint.color = pal.cursor.toArgb()
                canvas.drawRect(left, top, left + cellW, top + cellH, linePaint)
            }
        }

        canvas.restore()
    }

    private fun resolvePaint(mode: Int): TextPaint {
        val bold = mode and MODE_BOLD != 0
        val italic = mode and MODE_ITALIC != 0
        return when {
            bold && italic -> paintBoldItalic
            bold -> paintBold
            italic -> paintItalic
            else -> paintNormal
        }
    }

    private fun updatePaints(sizeSp: Int) {
        val sizePx = spToPx(sizeSp.toFloat())
        updatePaintsPx(sizePx)
    }

    private fun updatePaintsPx(sizePx: Float) {
        val minPx = spToPx(minFontSizeSp.toFloat())
        val maxPx = spToPx(MaxTerminalRenderFontSizeSp.toFloat())
        val clamped = sizePx.coerceIn(minPx, maxPx)
        val regular = ResourcesCompat.getFont(context, R.font.jetbrains_mono_regular)
        val bold = ResourcesCompat.getFont(context, R.font.jetbrains_mono_bold) ?: regular
        val italic = ResourcesCompat.getFont(context, R.font.jetbrains_mono_italic) ?: regular
        val boldItalic = ResourcesCompat.getFont(context, R.font.jetbrains_mono_bold_italic) ?: bold ?: italic

        paintNormal = TextPaint(Paint.ANTI_ALIAS_FLAG).apply {
            textSize = clamped
            textScaleX = 1f
            typeface = regular
        }
        paintBold = TextPaint(Paint.ANTI_ALIAS_FLAG).apply {
            textSize = clamped
            textScaleX = 1f
            typeface = bold ?: regular
        }
        paintItalic = TextPaint(Paint.ANTI_ALIAS_FLAG).apply {
            textSize = clamped
            textScaleX = 1f
            typeface = italic ?: regular
        }
        paintBoldItalic = TextPaint(Paint.ANTI_ALIAS_FLAG).apply {
            textSize = clamped
            textScaleX = 1f
            typeface = boldItalic ?: bold ?: italic ?: regular
        }

        updateCellMetrics()
    }

    private fun updateLayout() {
        val widthPx = width
        val heightPx = height
        if (widthPx <= 0 || heightPx <= 0) {
            lastViewportHeightPx = heightPx
            updateViewSize(0, 0)
            return
        }
        var minSp = minFontSizeSp
        if (fitToViewWidth && hostCols > 0) {
            minSp = 1
        }
        minFontSizeSp = minSp
        var desiredSp = baseFontSizeSp
        if (desiredSp <= 0) {
            desiredSp = minFontSizeSp
        }
        val baseSp = desiredSp.coerceAtLeast(minFontSizeSp)
        updatePaints(baseSp)

        val baseCellWidth = cellWidth
        val baseCellHeight = cellHeight
        if (baseCellWidth <= 0f || baseCellHeight <= 0f) {
            updateViewSize(0, 0)
            return
        }

        var scaleX = zoomFactor
        var scaleY = zoomFactor
        if (fitToViewWidth && hostCols > 0 && hostRows > 0) {
            val widthScale = widthPx.toFloat() / (hostCols.toFloat() * baseCellWidth)
            val fitScale = if (imeVisible) {
                widthScale
            } else {
                val heightScale = heightPx.toFloat() / (hostRows.toFloat() * baseCellHeight)
                minOf(widthScale, heightScale)
            }
            val effectiveScale = TerminalViewportPolicy.effectiveRenderScale(
                zoomFactor = zoomFactor,
                fitScale = fitScale,
            )
            scaleX = effectiveScale
            scaleY = effectiveScale
        }
        renderScaleX = scaleX
        renderScaleY = scaleY
        scaledCellWidth = baseCellWidth * renderScaleX
        scaledCellHeight = baseCellHeight * renderScaleY

        val nextCols = if (fitToViewWidth && hostCols > 0) {
            hostCols
        } else {
            floor(widthPx / scaledCellWidth).toInt().coerceAtLeast(0)
        }
        val nextRows = floor(heightPx / scaledCellHeight).toInt().coerceAtLeast(0)
        updateViewSize(nextCols, nextRows)
        val snap = snapshot
        if (snap != null && lastViewportHeightPx > 0 && lastViewportHeightPx != heightPx) {
            cameraOffsetYPx = if (TerminalViewportPolicy.shouldSnapToLiveBottom(
                    zoomFactor = zoomFactor,
                    scrollbackOffsetRows = scrollbackOffsetRows,
                )
            ) {
                if (snap.cursorVisible) {
                    TerminalViewportPolicy.autoFollowCursorCameraOffsetY(
                        cameraOffsetYPx = cameraOffsetYPx,
                        scaledCellHeightPx = scaledCellHeight,
                        viewportHeightPx = heightPx,
                        totalRows = snap.rows,
                        cursorY = snap.cursorY,
                    )
                } else {
                    TerminalViewportPolicy.bottomAlignedCameraOffsetY(
                        totalRows = snap.rows,
                        scaledCellHeightPx = scaledCellHeight,
                        viewportHeightPx = heightPx,
                    )
                }
            } else {
                TerminalViewportPolicy.preserveBottomAnchorOnHeightChange(
                    cameraOffsetYPx = cameraOffsetYPx,
                    previousViewportHeightPx = lastViewportHeightPx,
                    nextViewportHeightPx = heightPx,
                    totalRows = snap.rows,
                    scaledCellHeightPx = scaledCellHeight,
                )
            }
            scrollRemainderY = 0f
        }
        lastViewportHeightPx = heightPx
        clampCameraOffsets(resetScrollRemainder = true)
    }

    private fun updateViewSize(cols: Int, rows: Int) {
        if (viewCols == cols && viewRows == rows) return
        viewCols = cols
        viewRows = rows
        onViewSizeChanged?.invoke(cols, rows)
    }

    private fun resetPan() {
        cameraOffsetXPx = 0f
        cameraOffsetYPx = 0f
        scrollRemainderY = 0f
        pendingLiveReentryRows = 0
        pendingScrollbackEntryRows = 0
        pendingScrollbackEntryYPx = 0f
        cursorFollowAfterInput = false
        suppressCursorFollowForScrollbackReentry = false
    }

    private fun applyPanDelta(dx: Float, dy: Float) {
        if (scaledCellWidth <= 0f || scaledCellHeight <= 0f) return
        val snap = snapshot ?: return
        val maxOffsetXPx = max(0f, (snap.cols * scaledCellWidth) - width.toFloat())
        val maxOffsetYPx = max(0f, (snap.rows * scaledCellHeight) - height.toFloat())
        val attemptedX = cameraOffsetXPx - dx
        val attemptedY = cameraOffsetYPx - dy
        var overflowY = 0f
        if (attemptedY < 0f) {
            overflowY = attemptedY
        } else if (attemptedY > maxOffsetYPx) {
            overflowY = attemptedY - maxOffsetYPx
        }
        val nextX = attemptedX.coerceIn(0f, maxOffsetXPx)
        val nextY = attemptedY.coerceIn(0f, maxOffsetYPx)
        if (nextX == cameraOffsetXPx && nextY == cameraOffsetYPx && overflowY == 0f) return
        cameraOffsetXPx = nextX
        cameraOffsetYPx = nextY
        if (dy < 0f) {
            reducePendingScrollbackEntry(-dy)
        }
        maybeAutoReenterLiveView()
        dispatchScrollbackFromPanOverflow(overflowY)
        invalidate()
    }

    private fun clampCameraOffsets(resetScrollRemainder: Boolean) {
        val snap = snapshot ?: return
        if (scaledCellWidth <= 0f || scaledCellHeight <= 0f) return
        val cols = snap.cols
        val rows = snap.rows
        if (cols <= 0 || rows <= 0) return
        val maxOffsetXPx = max(0f, (cols * scaledCellWidth) - width.toFloat())
        val maxOffsetYPx = max(0f, (rows * scaledCellHeight) - height.toFloat())
        val nextX = cameraOffsetXPx.coerceIn(0f, maxOffsetXPx)
        val nextY = cameraOffsetYPx.coerceIn(0f, maxOffsetYPx)
        if (nextX != cameraOffsetXPx || nextY != cameraOffsetYPx) {
            cameraOffsetXPx = nextX
            cameraOffsetYPx = nextY
        }
        if (resetScrollRemainder) {
            scrollRemainderY = 0f
        }
    }

    private fun normalizeZoom(value: Float): Float {
        return value.coerceIn(MinTerminalZoom, MaxTerminalZoom)
    }

    private fun maybeAutoReenterLiveView() {
        if (scrollbackOffsetRows <= 0 || scaledCellHeight <= 0f) return
        pendingScrollbackEntryRows = 0
        pendingScrollbackEntryYPx = 0f
        if (pendingLiveReentryRows > 0) return
        val rowsToExit = TerminalViewportPolicy.scrollbackRowsToExitForLiveReentry(
            scrollbackOffsetRows = scrollbackOffsetRows,
            cameraOffsetYPx = cameraOffsetYPx,
            scaledCellHeightPx = scaledCellHeight,
        )
        if (rowsToExit <= 0) return
        pendingLiveReentryRows += rowsToExit
        onScrollback?.invoke(-rowsToExit)
    }

    private fun applyScrollbackOffsetChange(previousOffsetRows: Int, nextOffsetRows: Int) {
        val addedRows = (nextOffsetRows - previousOffsetRows).coerceAtLeast(0)
        if (addedRows > 0) {
            applyPendingScrollbackEntry(addedRows)
            return
        }
        applyPendingLiveReentry(previousOffsetRows = previousOffsetRows, nextOffsetRows = nextOffsetRows)
    }

    private fun reducePendingScrollbackEntry(deltaYPx: Float) {
        if (deltaYPx <= 0f || pendingScrollbackEntryYPx <= 0f || scaledCellHeight <= 0f) return
        pendingScrollbackEntryYPx = (pendingScrollbackEntryYPx - deltaYPx).coerceAtLeast(0f)
        pendingScrollbackEntryRows = if (pendingScrollbackEntryYPx > 0f) {
            ceil(pendingScrollbackEntryYPx / scaledCellHeight).toInt().coerceAtLeast(1)
        } else {
            0
        }
    }

    private fun applyPendingScrollbackEntry(addedRows: Int) {
        if (scaledCellHeight <= 0f) {
            pendingScrollbackEntryRows = 0
            pendingScrollbackEntryYPx = 0f
            return
        }
        val addedPx = addedRows * scaledCellHeight
        val pendingRowsToApply = min(addedRows, pendingScrollbackEntryRows)
        val pendingPxToApply = min(pendingScrollbackEntryYPx, pendingRowsToApply * scaledCellHeight)
        cameraOffsetYPx += addedPx - pendingPxToApply
        pendingScrollbackEntryRows = (pendingScrollbackEntryRows - pendingRowsToApply).coerceAtLeast(0)
        pendingScrollbackEntryYPx = (pendingScrollbackEntryYPx - pendingPxToApply).coerceAtLeast(0f)
        if (pendingScrollbackEntryRows == 0) {
            pendingScrollbackEntryYPx = 0f
        }
        scrollRemainderY = 0f
    }

    private fun applyPendingLiveReentry(previousOffsetRows: Int, nextOffsetRows: Int) {
        if (pendingLiveReentryRows <= 0) return
        val consumedRows = (previousOffsetRows - nextOffsetRows).coerceAtLeast(0)
        if (consumedRows <= 0) {
            pendingLiveReentryRows = 0
            return
        }
        val rowsToApply = min(pendingLiveReentryRows, consumedRows)
        if (scaledCellHeight > 0f) {
            cameraOffsetYPx = (cameraOffsetYPx - (rowsToApply * scaledCellHeight)).coerceAtLeast(0f)
        }
        pendingLiveReentryRows = (pendingLiveReentryRows - rowsToApply).coerceAtLeast(0)
        suppressCursorFollowForScrollbackReentry = true
        scrollRemainderY = 0f
    }

    private fun dispatchScrollbackFromPanOverflow(overflowYPx: Float) {
        if (overflowYPx == 0f || scaledCellHeight <= 0f) return
        if (overflowYPx < 0f) {
            pendingScrollbackEntryYPx += -overflowYPx
            val requiredRows = ceil(pendingScrollbackEntryYPx / scaledCellHeight).toInt().coerceAtLeast(1)
            val deltaRows = (requiredRows - pendingScrollbackEntryRows).coerceAtLeast(0)
            if (deltaRows == 0) return
            pendingScrollbackEntryRows += deltaRows
            onScrollback?.invoke(deltaRows)
            return
        }
        scrollRemainderY += -overflowYPx
        val deltaRows = (scrollRemainderY / scaledCellHeight).toInt()
        if (deltaRows == 0) return
        scrollRemainderY -= deltaRows * scaledCellHeight
        onScrollback?.invoke(deltaRows)
    }

    private fun spToPx(value: Float): Float {
        return TypedValue.applyDimension(
            TypedValue.COMPLEX_UNIT_SP,
            value,
            resources.displayMetrics,
        )
    }

    private fun updateCellMetrics() {
        val metrics = paintNormal.fontMetrics
        cellHeight = metrics.descent - metrics.ascent
        baseline = -metrics.ascent
        cellWidth = max(1f, paintNormal.measureText("M"))
    }

    private data class CellAttr(
        val mode: Int,
        val fg: Int,
        val bg: Int,
    )

    private fun cellWidth(grapheme: String): Int = if (grapheme.isNotEmpty()) 2 else 1

    private fun isContinuationCell(snapshot: TerminalSnapshot, idx: Int, attr: CellAttr): Boolean {
        if (idx < 0 || idx >= snapshot.runes.size) return false
        if (!snapshot.graphemes.isNullOrEmpty() && snapshot.graphemes[idx].isNotEmpty()) return false
        val rune = snapshot.runes[idx]
        if (rune != 0) return false
        val mode = snapshot.modes[idx]
        val fg = snapshot.fg[idx]
        val bg = snapshot.bg[idx]
        return mode == attr.mode && fg == attr.fg && bg == attr.bg
    }

    private companion object {
        const val zoomEpsilon = 0.001f
    }
}
