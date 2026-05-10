package systems.pkt.lingon.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.ime
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.ui.draw.alpha
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import android.content.Context
import android.text.Editable
import android.text.InputType
import android.util.Log
import android.view.InputDevice
import android.view.View
import android.view.ViewGroup
import android.view.inputmethod.BaseInputConnection
import android.view.inputmethod.EditorInfo
import android.view.inputmethod.InputConnection
import android.view.inputmethod.InputMethodManager
import android.view.KeyEvent as AndroidKeyEvent
import systems.pkt.lingon.BuildConfig
import systems.pkt.lingon.DefaultTerminalZoom
import systems.pkt.lingon.MinTerminalFontSizeSp
import systems.pkt.lingon.data.relay.RelaySession
import systems.pkt.lingon.terminal.TerminalGridView
import systems.pkt.lingon.terminal.TerminalPalette
import systems.pkt.lingon.terminal.TerminalViewportState
import systems.pkt.lingon.ui.theme.LocalLingonExtraColors
import systems.pkt.lingon.viewmodel.ConnectionState
import systems.pkt.lingon.viewmodel.AppViewModel
import systems.pkt.lingon.viewmodel.UiState
import kotlin.math.abs

private const val TerminalInputLogTag = "TerminalInputView"

@Composable
fun TerminalScreen(
    title: String,
    state: UiState,
    viewModel: AppViewModel,
    menuExpanded: Boolean,
    onToggleMenu: () -> Unit,
    onDismissMenu: () -> Unit,
) {
    var ctrlActive by remember { mutableStateOf(false) }
    var altActive by remember { mutableStateOf(false) }
    var requestInputFocus by remember { mutableStateOf<(() -> Unit)?>(null) }
    var requestInputBlur by remember { mutableStateOf<(() -> Unit)?>(null) }
    var inputReadyNonce by remember { mutableStateOf(0) }
    var terminalGridView by remember { mutableStateOf<TerminalGridView?>(null) }
    var imeRestoreInProgress by remember { mutableStateOf(false) }
    var captureImeInsetChanges by remember { mutableStateOf(true) }
    var observedTerminalImeVisible by remember { mutableStateOf(false) }
    val viewportCache = remember { mutableStateMapOf<String, TerminalViewportState>() }
    val config = LocalConfiguration.current
    val lifecycleOwner = LocalLifecycleOwner.current
    val isCompact = config.screenWidthDp < 360 || config.screenHeightDp < 700
    val isLandscape = config.screenWidthDp > config.screenHeightDp
    val screenPadding = if (isCompact) 8.dp else 12.dp
    val spacing = if (isCompact) 6.dp else 8.dp

    val imeVisible = isTerminalImeVisible(
        imeBottomPx = WindowInsets.ime.getBottom(LocalDensity.current),
        navigationBarsBottomPx = WindowInsets.navigationBars.getBottom(LocalDensity.current),
    )
    val palette = rememberTerminalPalette()
    val focusInput: () -> Unit = {
        viewModel.recordTerminalImeVisibilityForLifecycle(true)
        requestInputFocus?.invoke()
        Unit
    }
    fun maybeFocusInputForImeRestore(suppressHiddenCapture: Boolean) {
        if (state.restoreTerminalImeOnLifecycleStart == false) {
            imeRestoreInProgress = false
            requestInputBlur?.invoke()
        } else {
            imeRestoreInProgress = suppressHiddenCapture && state.restoreTerminalImeOnLifecycleStart == true
            focusInput()
        }
    }
    val focusInputIfImeRestoreAllowed: () -> Unit = {
        maybeFocusInputForImeRestore(suppressHiddenCapture = false)
    }
    val restoreInputFocusOnLifecycleStart: () -> Unit = {
        maybeFocusInputForImeRestore(suppressHiddenCapture = true)
    }
    LaunchedEffect(state.activeSessionId, inputReadyNonce) { focusInputIfImeRestoreAllowed() }
    DisposableEffect(lifecycleOwner) {
        val observer = LifecycleEventObserver { _, event ->
            when (event) {
                Lifecycle.Event.ON_PAUSE -> captureImeInsetChanges = false
                Lifecycle.Event.ON_RESUME -> captureImeInsetChanges = true
                else -> Unit
            }
        }
        lifecycleOwner.lifecycle.addObserver(observer)
        onDispose {
            lifecycleOwner.lifecycle.removeObserver(observer)
        }
    }
    LaunchedEffect(imeVisible, state.activeSessionId, captureImeInsetChanges) {
        if (
            !captureImeInsetChanges ||
            !lifecycleOwner.lifecycle.currentState.isAtLeast(Lifecycle.State.RESUMED)
        ) {
            return@LaunchedEffect
        }
        if (imeVisible) {
            observedTerminalImeVisible = true
            imeRestoreInProgress = false
            viewModel.recordTerminalImeVisibilityForLifecycle(true)
        } else if (!imeRestoreInProgress) {
            when (state.restoreTerminalImeOnLifecycleStart) {
                true -> {
                    requestInputBlur?.invoke()
                    viewModel.recordTerminalImeVisibilityForLifecycle(false)
                }
                false -> requestInputBlur?.invoke()
                null -> {
                    if (observedTerminalImeVisible) {
                        requestInputBlur?.invoke()
                        viewModel.recordTerminalImeVisibilityForLifecycle(false)
                    }
                }
            }
        }
    }

    val handleSelectSession: (String) -> Unit = { nextSessionId ->
        val currentSessionId = state.activeSessionId
        if (!currentSessionId.isNullOrBlank() && currentSessionId != nextSessionId) {
            terminalGridView?.let { view ->
                viewportCache[currentSessionId] = view.captureViewportState()
            }
        }
        viewModel.selectSession(nextSessionId)
    }

    fun sendTextFromSoftInput(payload: String) {
        val dispatch = dispatchSoftInput(payload, ctrlActive = ctrlActive, altActive = altActive)
        dispatch.text?.let { text ->
            viewModel.sendRawInput(text)
        }
        dispatch.bytes?.takeIf { it.isNotEmpty() }?.let { bytes ->
            viewModel.sendRawBytes(bytes)
        }
        ctrlActive = dispatch.nextCtrlActive
        altActive = dispatch.nextAltActive
    }

    fun sendBytesFromSoftInput(bytes: ByteArray) {
        if (bytes.isEmpty()) return
        viewModel.sendRawBytes(bytes)
        if (ctrlActive || altActive) {
            ctrlActive = false
            altActive = false
        }
    }

    fun handleHardwareKey(native: AndroidKeyEvent): Boolean {
        if (native.action != AndroidKeyEvent.ACTION_DOWN) return false
        if (!native.isFromSource(InputDevice.SOURCE_KEYBOARD)) return false

        val ctrlActiveNative = native.metaState and AndroidKeyEvent.META_CTRL_ON != 0
        val altActiveNative = native.metaState and AndroidKeyEvent.META_ALT_ON != 0

        val directBytes = hardwareKeyBytes(native.keyCode)
        if (directBytes != null && !ctrlActiveNative && !altActiveNative) {
            viewModel.sendRawBytes(directBytes)
            return true
        }

        val unicode = native.unicodeChar
        if (unicode != 0) {
            val text = String(Character.toChars(unicode))
            if (ctrlActiveNative || altActiveNative) {
                val bytes = buildModifiedBytes(text, ctrlActiveNative, altActiveNative)
                if (bytes.isNotEmpty()) {
                    viewModel.sendRawBytes(bytes)
                }
            } else {
                viewModel.sendRawInput(text)
            }
            return true
        }

        if (ctrlActiveNative || altActiveNative) {
            val mapped = keyCodeToAscii(native.keyCode)
            if (mapped != null) {
                val bytes = buildModifiedBytes(mapped.toString(), ctrlActiveNative, altActiveNative)
                if (bytes.isNotEmpty()) {
                    viewModel.sendRawBytes(bytes)
                }
                return true
            }
        }

        return false
    }

    Box(
        modifier = Modifier
            .fillMaxSize(),
    ) {
        if (isLandscape) {
            Row(
                modifier = Modifier
                    .fillMaxSize()
                    .imePadding(),
                horizontalArrangement = Arrangement.spacedBy(spacing),
            ) {
                Column(
                    modifier = Modifier
                        .fillMaxHeight()
                        .widthIn(min = 120.dp, max = 168.dp)
                        .padding(start = screenPadding, top = if (isCompact) 4.dp else 6.dp, bottom = screenPadding),
                    verticalArrangement = Arrangement.spacedBy(spacing),
                ) {
                    TopBar(
                        title = title,
                        username = state.username,
                        loggedIn = state.loggedIn,
                        menuExpanded = menuExpanded,
                        onToggleMenu = onToggleMenu,
                        onDismissMenu = onDismissMenu,
                        onShowSettings = { viewModel.showSettings(true) },
                        onShowTheme = { viewModel.showThemePicker(true) },
                        onShowAppLock = { viewModel.showAppLockTimeoutDialog(true) },
                        onResetZoomPan = { viewModel.resetZoomAndPan() },
                        wallInactivityEnabled = state.wallInactivityEnabled,
                        wallInactivityLabel = state.wallInactivityLabel,
                        wallInactivityAvailable = !state.activeSessionId.isNullOrBlank(),
                        onToggleWallInactivity = { viewModel.toggleWallInactivity() },
                        headlessResizeAvailable = !state.activeSessionId.isNullOrBlank(),
                        headlessResizeEnabled = state.activeSessionHeadless,
                        onResizeHeadlessNow = { viewModel.sendHeadlessResizeNow() },
                        onReload = { viewModel.manualRefresh() },
                        onShowShareToken = { viewModel.showShareToken(true, state.shareToken) },
                        onShowCertificates = { viewModel.showCertificates(true) },
                        backgroundWallEnabled = state.backgroundWallEnabled,
                        onToggleBackgroundWall = { enabled -> viewModel.setBackgroundWallEnabled(enabled) },
                        onLogout = { viewModel.logout() },
                        compact = true,
                        vertical = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                    SessionsColumn(
                        sessions = state.sessions,
                        activeSessionId = state.activeSessionId,
                        onSelect = handleSelectSession,
                        compact = true,
                    )
                }
                TerminalPanel(
                    state = state,
                    viewModel = viewModel,
                    viewportCache = viewportCache,
                    terminalGridView = terminalGridView,
                    onTerminalGridViewChanged = { terminalGridView = it },
                    palette = palette,
                    fitToViewWidth = true,
                    screenPadding = screenPadding,
                    isCompact = isCompact,
                    isLandscape = true,
                    imeVisible = imeVisible,
                    onHardwareKey = ::handleHardwareKey,
                    ctrlActive = ctrlActive,
                    altActive = altActive,
                    onToggleCtrl = { ctrlActive = !ctrlActive },
                    onToggleAlt = { altActive = !altActive },
                    onSendKey = ::sendTextFromSoftInput,
                    onSendBytes = ::sendBytesFromSoftInput,
                    onInputReady = { requestFocus ->
                        requestInputFocus = requestFocus
                        inputReadyNonce += 1
                    },
                    onInputBlurReady = { requestBlur ->
                        requestInputBlur = requestBlur
                    },
                    focusInput = focusInput,
                    focusInputIfImeRestoreAllowed = restoreInputFocusOnLifecycleStart,
                    showStatusOverlay = true,
                    modifier = Modifier
                        .weight(1f)
                        .fillMaxHeight(),
                )
            }
        } else {
            Column(
                modifier = Modifier
                    .fillMaxSize()
                    .imePadding(),
                verticalArrangement = Arrangement.spacedBy(spacing),
            ) {
                TopBar(
                    title = title,
                    username = state.username,
                    loggedIn = state.loggedIn,
                    menuExpanded = menuExpanded,
                    onToggleMenu = onToggleMenu,
                    onDismissMenu = onDismissMenu,
                    onShowSettings = { viewModel.showSettings(true) },
                    onShowTheme = { viewModel.showThemePicker(true) },
                    onShowAppLock = { viewModel.showAppLockTimeoutDialog(true) },
                    onResetZoomPan = { viewModel.resetZoomAndPan() },
                    wallInactivityEnabled = state.wallInactivityEnabled,
                    wallInactivityLabel = state.wallInactivityLabel,
                    wallInactivityAvailable = !state.activeSessionId.isNullOrBlank(),
                    onToggleWallInactivity = { viewModel.toggleWallInactivity() },
                    headlessResizeAvailable = !state.activeSessionId.isNullOrBlank(),
                    headlessResizeEnabled = state.activeSessionHeadless,
                    onResizeHeadlessNow = { viewModel.sendHeadlessResizeNow() },
                    onReload = { viewModel.manualRefresh() },
                    onShowShareToken = { viewModel.showShareToken(true, state.shareToken) },
                    onShowCertificates = { viewModel.showCertificates(true) },
                    backgroundWallEnabled = state.backgroundWallEnabled,
                    onToggleBackgroundWall = { enabled -> viewModel.setBackgroundWallEnabled(enabled) },
                    onLogout = { viewModel.logout() },
                    compact = isCompact,
                    vertical = false,
                )
                Column(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(horizontal = screenPadding)
                        .padding(top = if (isCompact) 0.dp else 4.dp),
                    verticalArrangement = Arrangement.spacedBy(spacing),
                ) {
                    SessionsRow(
                        sessions = state.sessions,
                        activeSessionId = state.activeSessionId,
                        onSelect = handleSelectSession,
                        compact = isCompact,
                    )
                    StatusBanner(status = state.bannerStatus)
                }
                TerminalPanel(
                    state = state,
                    viewModel = viewModel,
                    viewportCache = viewportCache,
                    terminalGridView = terminalGridView,
                    onTerminalGridViewChanged = { terminalGridView = it },
                    palette = palette,
                    fitToViewWidth = true,
                    screenPadding = screenPadding,
                    isCompact = isCompact,
                    isLandscape = false,
                    imeVisible = imeVisible,
                    onHardwareKey = ::handleHardwareKey,
                    ctrlActive = ctrlActive,
                    altActive = altActive,
                    onToggleCtrl = { ctrlActive = !ctrlActive },
                    onToggleAlt = { altActive = !altActive },
                    onSendKey = ::sendTextFromSoftInput,
                    onSendBytes = ::sendBytesFromSoftInput,
                    onInputReady = { requestFocus ->
                        requestInputFocus = requestFocus
                        inputReadyNonce += 1
                    },
                    onInputBlurReady = { requestBlur ->
                        requestInputBlur = requestBlur
                    },
                    focusInput = focusInput,
                    focusInputIfImeRestoreAllowed = restoreInputFocusOnLifecycleStart,
                    showStatusOverlay = false,
                    modifier = Modifier
                        .weight(1f)
                        .fillMaxWidth(),
                )
            }
        }
    }
}

@Composable
private fun SessionsRow(
    sessions: List<RelaySession>,
    activeSessionId: String?,
    onSelect: (String) -> Unit,
    compact: Boolean,
) {
    if (sessions.isEmpty()) return
    val listState = rememberLazyListState()
    val activeIndex = sessions.indexOfFirst { it.id == activeSessionId }
    val tabPadding = if (compact) 6.dp else 8.dp
    val tabVerticalPadding = if (compact) 4.dp else 6.dp
    val tabTextStyle = MaterialTheme.typography.labelMedium.copy(
        fontSize = if (compact) 10.sp else 11.sp,
        lineHeight = if (compact) 12.sp else 14.sp,
    )

    LaunchedEffect(activeIndex, sessions.size) {
        if (activeIndex >= 0) {
            listState.animateScrollToItem(activeIndex)
        }
    }

    Row(
        modifier = Modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        LazyRow(
            state = listState,
            horizontalArrangement = Arrangement.spacedBy(if (compact) 6.dp else 8.dp),
            modifier = Modifier
                .weight(1f)
                .testTag(TestTags.TabList),
        ) {
            itemsIndexed(sessions) { _, session ->
                val isActive = session.id == activeSessionId
                val label = session.name?.ifBlank { session.id } ?: session.id
                Surface(
                    shape = RoundedCornerShape(6.dp),
                    border = BorderStroke(
                        1.dp,
                        if (isActive) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurface.copy(alpha = 0.2f),
                    ),
                    color = MaterialTheme.colorScheme.surfaceVariant,
                    modifier = Modifier
                        .clickable { onSelect(session.id) }
                        .testTag(TestTags.tabTag(session.id))
                        .semantics { selected = isActive },
                ) {
                    Text(
                        text = label,
                        modifier = Modifier.padding(horizontal = tabPadding, vertical = tabVerticalPadding),
                        color = if (isActive) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.onSurface.copy(alpha = 0.6f),
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        style = tabTextStyle,
                    )
                }
            }
        }
    }
}

@Composable
private fun SessionsColumn(
    sessions: List<RelaySession>,
    activeSessionId: String?,
    onSelect: (String) -> Unit,
    compact: Boolean,
) {
    if (sessions.isEmpty()) return
    val listState = rememberLazyListState()
    val activeIndex = sessions.indexOfFirst { it.id == activeSessionId }
    val tabPadding = if (compact) 6.dp else 8.dp
    val tabVerticalPadding = if (compact) 4.dp else 6.dp
    val tabTextStyle = MaterialTheme.typography.labelMedium.copy(
        fontSize = if (compact) 10.sp else 11.sp,
        lineHeight = if (compact) 12.sp else 14.sp,
    )
    val extras = LocalLingonExtraColors.current

    LaunchedEffect(activeIndex) {
        if (activeIndex >= 0) {
            listState.animateScrollToItem(activeIndex)
        }
    }

    LazyColumn(
        state = listState,
        modifier = Modifier
            .fillMaxWidth()
            .testTag(TestTags.TabList),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        itemsIndexed(sessions) { _, session ->
            val label = session.name?.ifBlank { session.id } ?: session.id
            val selected = session.id == activeSessionId
            val activeColor = MaterialTheme.colorScheme.primary
            val background = if (selected) activeColor else extras.inputBg
            val border = if (selected) activeColor else extras.border
            val textColor = if (selected) MaterialTheme.colorScheme.onPrimary else extras.muted
            val tag = TestTags.tabTag(label)
            Surface(
                modifier = Modifier
                    .semantics { this.selected = selected }
                    .testTag(tag)
                    .clickable { onSelect(session.id) },
                color = background,
                shape = RoundedCornerShape(8.dp),
                border = BorderStroke(1.dp, border.copy(alpha = 0.6f)),
            ) {
                Row(
                    modifier = Modifier
                        .padding(horizontal = tabPadding, vertical = tabVerticalPadding),
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        text = label,
                        style = tabTextStyle,
                        color = textColor,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }
        }
    }
}

@Composable
private fun TerminalPanel(
    state: UiState,
    viewModel: AppViewModel,
    viewportCache: MutableMap<String, TerminalViewportState>,
    terminalGridView: TerminalGridView?,
    onTerminalGridViewChanged: (TerminalGridView?) -> Unit,
    palette: TerminalPalette,
    fitToViewWidth: Boolean,
    screenPadding: Dp,
    isCompact: Boolean,
    isLandscape: Boolean,
    imeVisible: Boolean,
    onHardwareKey: (AndroidKeyEvent) -> Boolean,
    ctrlActive: Boolean,
    altActive: Boolean,
    onToggleCtrl: () -> Unit,
    onToggleAlt: () -> Unit,
    onSendKey: (String) -> Unit,
    onSendBytes: (ByteArray) -> Unit,
    onInputReady: ((() -> Unit) -> Unit),
    onInputBlurReady: ((() -> Unit) -> Unit),
    focusInput: () -> Unit,
    focusInputIfImeRestoreAllowed: () -> Unit,
    showStatusOverlay: Boolean,
    modifier: Modifier = Modifier,
) {
    Column(modifier = modifier.testTag(TestTags.TerminalFocus)) {
        var restoredSessionId by remember { mutableStateOf<String?>(null) }
        var lifecycleRestoreNonce by remember { mutableStateOf(0) }
        var restoredLifecycleNonce by remember { mutableStateOf(0) }
        val sessionId = state.activeSessionId
        val defaultLiveZoom = abs(state.zoomFactor - DefaultTerminalZoom) < 0.001f
        val shouldDelayViewportRestore = state.sessionSyncing && defaultLiveZoom && state.scrollbackOffsetRows == 0
        val lifecycleOwner = LocalLifecycleOwner.current
        val currentFocusInputIfImeRestoreAllowed by rememberUpdatedState(focusInputIfImeRestoreAllowed)
        DisposableEffect(lifecycleOwner, terminalGridView, sessionId) {
            val observer = LifecycleEventObserver { _, event ->
                val activeSessionId = sessionId ?: return@LifecycleEventObserver
                val view = terminalGridView
                when (event) {
                    Lifecycle.Event.ON_STOP -> {
                        if (view != null) {
                            viewportCache[activeSessionId] = view.captureViewportState()
                        }
                    }
                    Lifecycle.Event.ON_START -> {
                        lifecycleRestoreNonce += 1
                        currentFocusInputIfImeRestoreAllowed()
                    }
                    else -> Unit
                }
            }
            lifecycleOwner.lifecycle.addObserver(observer)
            onDispose {
                lifecycleOwner.lifecycle.removeObserver(observer)
            }
        }
        Box(
            modifier = Modifier
                .fillMaxWidth()
                .weight(1f),
        ) {
            val hostCols = state.activeSnapshot?.cols ?: 0
            val hostRows = state.activeSnapshot?.let { snapshot ->
                (snapshot.rows - state.scrollbackOffsetRows).coerceAtLeast(0)
            } ?: 0
            AndroidView(
                factory = { context ->
                    TerminalGridView(context).apply {
                        onTerminalGridViewChanged(this)
                        tag = "terminal_view"
                        setOnZoomChanged { value ->
                            viewModel.updateZoomFactor(value)
                        }
                        setOnTap { focusInput() }
                        setOnScrollback { deltaRows ->
                            viewModel.adjustScrollback(deltaRows)
                        }
                    }
                },
                update = { view ->
                    onTerminalGridViewChanged(view)
                    view.update(
                        snapshot = state.activeSnapshot,
                        fontSizeSp = state.fontSizeSp,
                        minFontSizeSp = MinTerminalFontSizeSp,
                        palette = palette,
                        frameSeq = state.lastFrameSeq,
                        hostCols = hostCols,
                        hostRows = hostRows,
                        fitToViewWidth = fitToViewWidth,
                        zoomFactor = state.zoomFactor,
                        panResetNonce = state.panResetNonce,
                        scrollbackOffsetRows = state.scrollbackOffsetRows,
                        imeVisible = imeVisible,
                        isLoading = state.sessionSyncing || state.connectionState == ConnectionState.Connecting,
                    )
                    view.setOnViewSizeChanged { cols, rows ->
                        if (cols <= 0 || rows <= 0) return@setOnViewSizeChanged
                        viewModel.updateTerminalSize(cols, rows)
                    }
                },
                modifier = Modifier
                    .fillMaxSize()
                    .testTag(TestTags.TerminalList),
            )
            LaunchedEffect(sessionId) {
                restoredSessionId = null
            }
            LaunchedEffect(
                sessionId,
                terminalGridView,
                state.sessionSyncing,
                state.lastFrameSeq,
                state.scrollbackOffsetRows,
                fitToViewWidth,
                lifecycleRestoreNonce,
            ) {
                val view = terminalGridView ?: return@LaunchedEffect
                val activeSessionId = sessionId ?: return@LaunchedEffect
                if (
                    restoredSessionId == activeSessionId &&
                    restoredLifecycleNonce == lifecycleRestoreNonce
                ) {
                    return@LaunchedEffect
                }
                if (shouldDelayViewportRestore) return@LaunchedEffect
                view.scheduleViewportRestore(viewportCache[activeSessionId])
                restoredSessionId = activeSessionId
                restoredLifecycleNonce = lifecycleRestoreNonce
            }
            if (showStatusOverlay) {
                StatusBanner(
                    status = state.bannerStatus,
                    onDismiss = { viewModel.dismissStatus() },
                    modifier = Modifier
                        .align(Alignment.TopCenter)
                        .statusBarsPadding()
                        .padding(horizontal = screenPadding, vertical = screenPadding),
                )
            }
            if (state.showsSyncingIndicator) {
                Surface(
                    modifier = Modifier
                        .align(Alignment.TopEnd)
                        .padding(
                            top = screenPadding,
                            bottom = screenPadding,
                            start = screenPadding,
                            end = screenPadding,
                        ),
                    shape = RoundedCornerShape(999.dp),
                    color = MaterialTheme.colorScheme.surface.copy(alpha = 0.92f),
                    tonalElevation = 2.dp,
                    shadowElevation = 4.dp,
                ) {
                    Row(
                        modifier = Modifier.padding(horizontal = 10.dp, vertical = 6.dp),
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        CircularProgressIndicator(
                            modifier = Modifier.size(12.dp),
                            strokeWidth = 2.dp,
                        )
                        Text(
                            text = "syncing",
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurface,
                        )
                    }
                }
            }
            AndroidView(
                factory = { context ->
                    TerminalInputView(context).apply {
                        setOnCommitText { text ->
                            onSendKey(text)
                        }
                        setOnBackspace { count ->
                            if (count > 0) {
                                onSendBytes(ByteArray(count) { 0x7f.toByte() })
                            }
                        }
                        setOnEnter {
                            onSendBytes(byteArrayOf('\r'.code.toByte()))
                        }
                        setOnHardwareKey(onHardwareKey)
                        setOnClickListener {
                            focusInput()
                        }
                        onInputReady {
                            requestTerminalFocus()
                        }
                        onInputBlurReady {
                            clearTerminalFocus()
                        }
                    }
                },
                update = { view ->
                    view.setOnCommitText { text ->
                        onSendKey(text)
                    }
                    view.setOnBackspace { count ->
                        if (count > 0) {
                            onSendBytes(ByteArray(count) { 0x7f.toByte() })
                        }
                    }
                    view.setOnEnter {
                        onSendBytes(byteArrayOf('\r'.code.toByte()))
                    }
                    view.setOnHardwareKey(onHardwareKey)
                },
                modifier = Modifier
                    .size(1.dp)
                    .alpha(0f)
                    .testTag(TestTags.TerminalInput),
            )
        }
        if (imeVisible) {
            val verticalPadding = if (isLandscape) 4.dp else if (isCompact) 6.dp else 8.dp
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .navigationBarsPadding()
                    .padding(horizontal = screenPadding, vertical = verticalPadding),
            ) {
                QuickKeysRow(
                    compact = isCompact,
                    dense = isLandscape,
                    singleRow = isLandscape,
                    ctrlActive = ctrlActive,
                    altActive = altActive,
                    onToggleCtrl = onToggleCtrl,
                    onToggleAlt = onToggleAlt,
                    onSendKey = { key ->
                        focusInput()
                        onSendKey(key)
                    },
                    onSendBytes = { bytes ->
                        focusInput()
                        onSendBytes(bytes)
                    },
                )
            }
        }
    }
}

@Composable
private fun QuickKeysRow(
    compact: Boolean,
    dense: Boolean,
    singleRow: Boolean,
    ctrlActive: Boolean,
    altActive: Boolean,
    onToggleCtrl: () -> Unit,
    onToggleAlt: () -> Unit,
    onSendKey: (String) -> Unit,
    onSendBytes: (ByteArray) -> Unit,
) {
    val keyHeight = when {
        dense -> 24.dp
        compact -> 32.dp
        else -> 36.dp
    }
    val keyPadding = when {
        dense -> 2.dp
        compact -> 4.dp
        else -> 6.dp
    }
    val keyShape = RoundedCornerShape(4.dp)
    val keyTextStyle = MaterialTheme.typography.labelMedium.copy(fontWeight = FontWeight.SemiBold)
    val rowSpacing = when {
        dense -> 4.dp
        compact -> 6.dp
        else -> 8.dp
    }
    val keySpacing = when {
        dense -> 2.dp
        compact -> 4.dp
        else -> 6.dp
    }

    Column(
        modifier = Modifier.fillMaxWidth(),
        verticalArrangement = Arrangement.spacedBy(rowSpacing),
    ) {
        if (singleRow) {
            QuickKeyRow(
                keys = listOf(
                    QuickKeySpec("ESC") { onSendBytes(byteArrayOf(0x1b)) },
                    QuickKeySpec("TAB") { onSendKey("\t") },
                    QuickKeySpec("CTRL", active = ctrlActive) { onToggleCtrl() },
                    QuickKeySpec("ALT", active = altActive) { onToggleAlt() },
                    QuickKeySpec("←") { onSendKey("\u001b[D") },
                    QuickKeySpec("↓") { onSendKey("\u001b[B") },
                    QuickKeySpec("↑") { onSendKey("\u001b[A") },
                    QuickKeySpec("→") { onSendKey("\u001b[C") },
                ),
                height = keyHeight,
                padding = keyPadding,
                textStyle = keyTextStyle,
                spacing = keySpacing,
            )
        } else {
            QuickKeyRow(
                keys = listOf(
                    QuickKeySpec("ESC") { onSendBytes(byteArrayOf(0x1b)) },
                    QuickKeySpec("/") { onSendKey("/") },
                    QuickKeySpec("-") { onSendKey("-") },
                    QuickKeySpec("HOME") { onSendKey("\u001b[H") },
                    QuickKeySpec("↑") { onSendKey("\u001b[A") },
                    QuickKeySpec("END") { onSendKey("\u001b[F") },
                    QuickKeySpec("PGUP") { onSendKey("\u001b[5~") },
                ),
                height = keyHeight,
                padding = keyPadding,
                textStyle = keyTextStyle,
                spacing = keySpacing,
            )
            QuickKeyRow(
                keys = listOf(
                    QuickKeySpec("TAB") { onSendKey("\t") },
                    QuickKeySpec("CTRL", active = ctrlActive) { onToggleCtrl() },
                    QuickKeySpec("ALT", active = altActive) { onToggleAlt() },
                    QuickKeySpec("←") { onSendKey("\u001b[D") },
                    QuickKeySpec("↓") { onSendKey("\u001b[B") },
                    QuickKeySpec("→") { onSendKey("\u001b[C") },
                    QuickKeySpec("PGDN") { onSendKey("\u001b[6~") },
                ),
                height = keyHeight,
                padding = keyPadding,
                textStyle = keyTextStyle,
                spacing = keySpacing,
            )
        }
    }
}

@Composable
private fun QuickKeyRow(
    keys: List<QuickKeySpec>,
    height: androidx.compose.ui.unit.Dp,
    padding: androidx.compose.ui.unit.Dp,
    textStyle: TextStyle,
    spacing: androidx.compose.ui.unit.Dp,
) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(spacing),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        keys.forEach { key ->
            QuickKey(
                label = key.label,
                active = key.active,
                height = height,
                padding = padding,
                textStyle = textStyle,
                modifier = Modifier.weight(1f),
                onClick = key.onClick,
            )
        }
    }
}

@Composable
private fun QuickKey(
    label: String,
    active: Boolean,
    height: androidx.compose.ui.unit.Dp,
    padding: androidx.compose.ui.unit.Dp,
    textStyle: TextStyle,
    modifier: Modifier,
    onClick: () -> Unit,
) {
    val shape = RoundedCornerShape(4.dp)
    val borderColor = if (active) {
        MaterialTheme.colorScheme.primary
    } else {
        MaterialTheme.colorScheme.onSurface.copy(alpha = 0.2f)
    }
    val containerColor = if (active) {
        MaterialTheme.colorScheme.primary.copy(alpha = 0.16f)
    } else {
        MaterialTheme.colorScheme.surfaceVariant
    }

    Surface(
        modifier = modifier
            .height(height)
            .clickable(onClick = onClick),
        shape = shape,
        color = containerColor,
        border = BorderStroke(1.dp, borderColor),
    ) {
        Row(
            modifier = Modifier.padding(horizontal = padding),
            horizontalArrangement = Arrangement.Center,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(text = label, style = textStyle, maxLines = 1)
        }
    }
}

@Composable
private fun rememberTerminalPalette(): TerminalPalette {
    val extras = LocalLingonExtraColors.current
    val colorScheme = MaterialTheme.colorScheme
    return remember(extras, colorScheme) {
        TerminalPalette(
            defaultFg = colorScheme.onSurface,
            defaultBg = Color.Black,
            cursor = colorScheme.primary,
        )
    }
}

private data class QuickKeySpec(
    val label: String,
    val active: Boolean = false,
    val onClick: () -> Unit,
)

private class TerminalInputView(
    context: Context,
) : View(context) {
    private val inputBuffer = Editable.Factory.getInstance().newEditable("")
    private var onCommitTextCallback: (String) -> Unit = {}
    private var onBackspaceCallback: (Int) -> Unit = {}
    private var onEnterCallback: () -> Unit = {}
    private var onHardwareKeyCallback: (AndroidKeyEvent) -> Boolean = { false }
    private var focusRequestGeneration: Int = 0

    init {
        layoutParams = ViewGroup.LayoutParams(1, 1)
        isFocusable = true
        isFocusableInTouchMode = true
        setBackgroundColor(android.graphics.Color.TRANSPARENT)
        importantForAutofill = View.IMPORTANT_FOR_AUTOFILL_NO_EXCLUDE_DESCENDANTS
    }

    override fun onCheckIsTextEditor(): Boolean {
        return true
    }

    override fun onCreateInputConnection(outAttrs: EditorInfo): InputConnection {
        outAttrs.inputType = InputType.TYPE_CLASS_TEXT or
            InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS or
            InputType.TYPE_TEXT_FLAG_MULTI_LINE
        outAttrs.imeOptions = EditorInfo.IME_FLAG_NO_FULLSCREEN or EditorInfo.IME_ACTION_NONE
        return object : BaseInputConnection(this, true) {
            override fun getEditable(): Editable {
                return this@TerminalInputView.inputBuffer
            }

            override fun commitText(text: CharSequence?, newCursorPosition: Int): Boolean {
                if (!text.isNullOrEmpty()) {
                    if (BuildConfig.DEBUG) {
                        Log.d(TerminalInputLogTag, "commitText len=${text.length}")
                    }
                    emitCommittedText(text)
                }
                this@TerminalInputView.inputBuffer.clear()
                return true
            }

            override fun finishComposingText(): Boolean {
                val buffer = this@TerminalInputView.inputBuffer
                if (buffer.isNotEmpty()) {
                    if (BuildConfig.DEBUG) {
                        Log.d(TerminalInputLogTag, "finishComposingText len=${buffer.length}")
                    }
                    emitCommittedText(buffer)
                    buffer.clear()
                }
                return true
            }

            override fun deleteSurroundingText(leftLength: Int, rightLength: Int): Boolean {
                if (leftLength > 0 && this@TerminalInputView.inputBuffer.isNotEmpty()) {
                    deleteFromComposingBuffer(leftLength, codePoints = false)
                    return true
                }
                if (
                    shouldForwardImeDeleteSurroundingTextAsBackspace(leftLength, rightLength) &&
                    this@TerminalInputView.inputBuffer.isEmpty()
                ) {
                    onBackspaceCallback(leftLength)
                    return true
                }
                return super.deleteSurroundingText(leftLength, rightLength)
            }

            override fun deleteSurroundingTextInCodePoints(leftLength: Int, rightLength: Int): Boolean {
                if (leftLength > 0 && this@TerminalInputView.inputBuffer.isNotEmpty()) {
                    deleteFromComposingBuffer(leftLength, codePoints = true)
                    return true
                }
                if (
                    shouldForwardImeDeleteSurroundingTextAsBackspace(leftLength, rightLength) &&
                    this@TerminalInputView.inputBuffer.isEmpty()
                ) {
                    onBackspaceCallback(leftLength)
                    return true
                }
                return super.deleteSurroundingTextInCodePoints(leftLength, rightLength)
            }

            override fun sendKeyEvent(event: AndroidKeyEvent): Boolean {
                if (event.action != AndroidKeyEvent.ACTION_DOWN) {
                    return true
                }
                return when (event.keyCode) {
                    AndroidKeyEvent.KEYCODE_ENTER -> {
                        onEnterCallback()
                        inputBuffer.clear()
                        true
                    }
                    AndroidKeyEvent.KEYCODE_DEL -> {
                        onBackspaceCallback(1)
                        true
                    }
                    AndroidKeyEvent.KEYCODE_TAB -> {
                        onCommitTextCallback("\t")
                        true
                    }
                    else -> onHardwareKeyCallback(event)
                }
            }

        }
    }

    override fun onKeyDown(keyCode: Int, event: AndroidKeyEvent?): Boolean {
        if (event == null) {
            return false
        }
        if (onHardwareKeyCallback(event)) {
            return true
        }
        return super.onKeyDown(keyCode, event)
    }

    override fun dispatchKeyEventPreIme(event: AndroidKeyEvent): Boolean {
        if (event.keyCode == AndroidKeyEvent.KEYCODE_BACK) {
            if (event.action == AndroidKeyEvent.ACTION_DOWN) {
                focusRequestGeneration++
            } else if (event.action == AndroidKeyEvent.ACTION_UP) {
                clearTerminalFocus()
            }
        }
        return super.dispatchKeyEventPreIme(event)
    }

    fun setOnCommitText(listener: (String) -> Unit) {
        onCommitTextCallback = listener
    }

    fun setOnBackspace(listener: (Int) -> Unit) {
        onBackspaceCallback = listener
    }

    fun setOnEnter(listener: () -> Unit) {
        onEnterCallback = listener
    }

    fun setOnHardwareKey(listener: (AndroidKeyEvent) -> Boolean) {
        onHardwareKeyCallback = listener
    }

    fun requestTerminalFocus() {
        val generation = ++focusRequestGeneration
        if (!hasFocus()) {
            requestFocus()
        }
        fun showKeyboardIfCurrent() {
            if (generation != focusRequestGeneration || !hasFocus()) {
                return
            }
            try {
                val imm = context.getSystemService(InputMethodManager::class.java)
                imm?.restartInput(this)
                imm?.showSoftInput(this, InputMethodManager.SHOW_IMPLICIT)
            } catch (_: RuntimeException) {
                // Some OEM/emulator IMEs can throw while view focus is transitioning.
            }
        }
        post { showKeyboardIfCurrent() }
        postDelayed({ showKeyboardIfCurrent() }, 120L)
        postDelayed({ showKeyboardIfCurrent() }, 360L)
    }

    fun clearTerminalFocus() {
        focusRequestGeneration++
        try {
            val imm = context.getSystemService(InputMethodManager::class.java)
            imm?.hideSoftInputFromWindow(windowToken, 0)
        } catch (_: RuntimeException) {
            // Some OEM/emulator IMEs can throw while view focus is transitioning.
        }
        clearFocus()
    }

    private fun emitCommittedText(text: CharSequence) {
        if (text.isEmpty()) return
        val builder = StringBuilder()
        text.forEach { ch ->
            if (ch == '\n') {
                if (builder.isNotEmpty()) {
                    onCommitTextCallback(builder.toString())
                    builder.clear()
                }
                onEnterCallback()
            } else {
                builder.append(ch)
            }
        }
        if (builder.isNotEmpty()) {
            onCommitTextCallback(builder.toString())
        }
    }

    private fun deleteFromComposingBuffer(leftLength: Int, codePoints: Boolean) {
        val buffer = this@TerminalInputView.inputBuffer
        if (buffer.isEmpty() || leftLength <= 0) {
            return
        }

        val deleteCount = if (codePoints) {
            minOf(leftLength, Character.codePointCount(buffer, 0, buffer.length))
        } else {
            minOf(leftLength, buffer.length)
        }
        if (deleteCount <= 0) {
            return
        }

        val start = if (codePoints) {
            Character.offsetByCodePoints(buffer, buffer.length, -deleteCount)
        } else {
            buffer.length - deleteCount
        }
        buffer.delete(start, buffer.length)
    }
}

private fun keyCodeToAscii(keyCode: Int): Char? {
    return when (keyCode) {
        AndroidKeyEvent.KEYCODE_A -> 'a'
        AndroidKeyEvent.KEYCODE_B -> 'b'
        AndroidKeyEvent.KEYCODE_C -> 'c'
        AndroidKeyEvent.KEYCODE_D -> 'd'
        AndroidKeyEvent.KEYCODE_E -> 'e'
        AndroidKeyEvent.KEYCODE_F -> 'f'
        AndroidKeyEvent.KEYCODE_G -> 'g'
        AndroidKeyEvent.KEYCODE_H -> 'h'
        AndroidKeyEvent.KEYCODE_I -> 'i'
        AndroidKeyEvent.KEYCODE_J -> 'j'
        AndroidKeyEvent.KEYCODE_K -> 'k'
        AndroidKeyEvent.KEYCODE_L -> 'l'
        AndroidKeyEvent.KEYCODE_M -> 'm'
        AndroidKeyEvent.KEYCODE_N -> 'n'
        AndroidKeyEvent.KEYCODE_O -> 'o'
        AndroidKeyEvent.KEYCODE_P -> 'p'
        AndroidKeyEvent.KEYCODE_Q -> 'q'
        AndroidKeyEvent.KEYCODE_R -> 'r'
        AndroidKeyEvent.KEYCODE_S -> 's'
        AndroidKeyEvent.KEYCODE_T -> 't'
        AndroidKeyEvent.KEYCODE_U -> 'u'
        AndroidKeyEvent.KEYCODE_V -> 'v'
        AndroidKeyEvent.KEYCODE_W -> 'w'
        AndroidKeyEvent.KEYCODE_X -> 'x'
        AndroidKeyEvent.KEYCODE_Y -> 'y'
        AndroidKeyEvent.KEYCODE_Z -> 'z'
        AndroidKeyEvent.KEYCODE_SPACE -> ' '
        else -> null
    }
}
