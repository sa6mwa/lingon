package systems.pkt.lingon.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.waitForUpOrCancellation
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
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.onClick
import androidx.compose.ui.semantics.onLongClick
import androidx.compose.ui.semantics.role
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
import android.app.Activity
import android.content.ActivityNotFoundException
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.net.Uri
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
import android.widget.Toast
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

object TerminalLinkOpener {
    fun createIntent(context: Context, url: String): Intent {
        return Intent(Intent.ACTION_VIEW, Uri.parse(url)).apply {
            addCategory(Intent.CATEGORY_BROWSABLE)
            if (context !is Activity) {
                addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            }
        }
    }

    fun open(context: Context, url: String): Boolean {
        return try {
            context.startActivity(createIntent(context, url))
            true
        } catch (err: ActivityNotFoundException) {
            Toast.makeText(context, "No app can open this link", Toast.LENGTH_SHORT).show()
            Log.w(TerminalInputLogTag, "No activity found for terminal link", err)
            false
        }
    }
}

@Composable
fun TerminalScreen(
    title: String,
    state: UiState,
    viewModel: AppViewModel,
    onOpenSettings: () -> Unit,
    viewportCache: MutableMap<String, TerminalViewportState>,
    viewportCacheIdentity: String?,
) {
    var ctrlActive by remember { mutableStateOf(false) }
    var altMode by remember { mutableStateOf(AltButtonMode.Off) }
    var fnShiftActive by remember { mutableStateOf(false) }
    var fnModifierMode by remember { mutableStateOf(FnModifierMode.Off) }
    val selectionMode = altMode == AltButtonMode.Fn
    var activeGridView by remember { mutableStateOf<TerminalGridView?>(null) }
    val context = LocalContext.current
    val clipboard = remember(context) { context.getSystemService(ClipboardManager::class.java) }
    var requestInputFocus by remember { mutableStateOf<(() -> Unit)?>(null) }
    var requestInputBlur by remember { mutableStateOf<(() -> Unit)?>(null) }
    var inputReadyNonce by remember { mutableStateOf(0) }
    var imeRestoreInProgress by remember { mutableStateOf(false) }
    var userImeDismissInProgress by remember { mutableStateOf(false) }
    var captureImeInsetChanges by remember { mutableStateOf(true) }
    var observedTerminalImeVisible by remember { mutableStateOf(false) }
    val config = LocalConfiguration.current
    val lifecycleOwner = LocalLifecycleOwner.current
    val isCompact = config.screenWidthDp < 360 || config.screenHeightDp < 700
    val isLandscape = config.screenWidthDp > config.screenHeightDp
    val screenPadding = if (isCompact) 8.dp else 12.dp
    val spacing = if (isCompact) 6.dp else 8.dp

    val rawImeVisible = isTerminalImeVisible(
        imeBottomPx = WindowInsets.ime.getBottom(LocalDensity.current),
        navigationBarsBottomPx = WindowInsets.navigationBars.getBottom(LocalDensity.current),
    )
    val waitingForRestoredImeInset =
        state.restoreTerminalImeOnLifecycleStart == true &&
            !rawImeVisible &&
            !userImeDismissInProgress &&
            (
                !observedTerminalImeVisible ||
                    state.sessionSyncing ||
                    state.connectionState != ConnectionState.Connected
                )
    val imeVisible =
        rawImeVisible ||
            (
                !waitingForRestoredImeInset &&
                state.restoreTerminalImeOnLifecycleStart == true &&
                    !userImeDismissInProgress &&
                    observedTerminalImeVisible
                )
    val palette = rememberTerminalPalette()
    val focusInput: () -> Unit = {
        userImeDismissInProgress = false
        viewModel.recordTerminalImeVisibilityForLifecycle(true)
        requestInputFocus?.invoke()
        Unit
    }
    fun maybeFocusInputForImeRestore(suppressHiddenCapture: Boolean) {
        if (state.restoreTerminalImeOnLifecycleStart == false) {
            imeRestoreInProgress = false
            requestInputBlur?.invoke()
        } else if (imeVisible) {
            imeRestoreInProgress = false
            userImeDismissInProgress = false
            observedTerminalImeVisible = true
            viewModel.recordTerminalImeVisibilityForLifecycle(true)
        } else if (suppressHiddenCapture && state.restoreTerminalImeOnLifecycleStart == true) {
            imeRestoreInProgress = true
        } else {
            imeRestoreInProgress = false
            focusInput()
        }
    }
    val focusInputIfImeRestoreAllowed: () -> Unit = {
        maybeFocusInputForImeRestore(suppressHiddenCapture = false)
    }
    val handleUserDismissIme: () -> Unit = {
        imeRestoreInProgress = false
        userImeDismissInProgress = true
        viewModel.recordTerminalImeVisibilityForLifecycle(false)
    }
    LaunchedEffect(state.activeSessionId) {
        activeGridView = null
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
    LaunchedEffect(
        rawImeVisible,
        state.activeSessionId,
        captureImeInsetChanges,
        state.restoreTerminalImeOnLifecycleStart,
        imeRestoreInProgress,
        observedTerminalImeVisible,
        userImeDismissInProgress,
    ) {
        when (
            decideTerminalImeLifecycleAction(
                TerminalImeLifecycleInput(
                    imeVisible = rawImeVisible,
                    captureImeInsetChanges = captureImeInsetChanges,
                    lifecycleResumed = lifecycleOwner.lifecycle.currentState.isAtLeast(Lifecycle.State.RESUMED),
                    restoreTerminalImeOnLifecycleStart = state.restoreTerminalImeOnLifecycleStart,
                    imeRestoreInProgress = imeRestoreInProgress,
                    observedTerminalImeVisible = observedTerminalImeVisible,
                    userDismissInProgress = userImeDismissInProgress,
                ),
            )
        ) {
            TerminalImeLifecycleAction.Ignore -> Unit
            TerminalImeLifecycleAction.RecordVisible -> {
                observedTerminalImeVisible = true
                imeRestoreInProgress = false
                viewModel.recordTerminalImeVisibilityForLifecycle(true)
            }
            TerminalImeLifecycleAction.BlurOnly -> requestInputBlur?.invoke()
            TerminalImeLifecycleAction.CompleteUserDismissAndBlur -> {
                userImeDismissInProgress = false
                requestInputBlur?.invoke()
            }
            TerminalImeLifecycleAction.RecordHiddenAndBlur -> {
                requestInputBlur?.invoke()
                viewModel.recordTerminalImeVisibilityForLifecycle(false)
            }
        }
    }

    val handleSelectSession: (String) -> Unit = { nextSessionId ->
        activeGridView = null
        viewModel.selectSession(nextSessionId)
    }

    fun sendTextFromSoftInput(payload: String) {
        if (altMode == AltButtonMode.Fn) {
            if (payload == "\r" || payload == "\n") {
                viewModel.sendRawBytes(byteArrayOf('\r'.code.toByte()))
                return
            }
            if (payload.startsWith("\u001b[")) {
                viewModel.sendRawBytes(payload.encodeToByteArray())
                return
            }
            payload.forEach { key ->
                when (val fnInput = dispatchFnKey(
                    key,
                    shiftActive = fnShiftActive,
                    ctrlActive = fnModifierMode.ctrlActive,
                    altActive = fnModifierMode.altActive,
                )) {
                    FnInput.Copy -> {
                        activeGridView?.selectedText()?.takeIf { it.isNotEmpty() }?.let { copied ->
                            clipboard?.setPrimaryClip(ClipData.newPlainText("Lingon terminal", copied))
                        }
                    }
                    FnInput.Paste -> {
                        val pasted = clipboard?.primaryClip?.takeIf { it.itemCount > 0 }
                            ?.getItemAt(0)?.coerceToText(context)?.toString()
                        if (!pasted.isNullOrEmpty()) {
                            viewModel.sendRawInput(pasted)
                        }
                    }
                    is FnInput.FunctionKey -> viewModel.sendRawBytes(fnInput.bytes)
                    is FnInput.Literal -> viewModel.sendRawInput(fnInput.text)
                }
            }
            return
        }
        val dispatch = dispatchSoftInput(payload, ctrlActive = ctrlActive, altActive = altMode == AltButtonMode.Alt)
        dispatch.text?.let { text ->
            viewModel.sendRawInput(text)
        }
        dispatch.bytes?.takeIf { it.isNotEmpty() }?.let { bytes ->
            viewModel.sendRawBytes(bytes)
        }
        ctrlActive = dispatch.nextCtrlActive
        if (altMode == AltButtonMode.Alt && !dispatch.nextAltActive) altMode = AltButtonMode.Off
    }

    fun sendBytesFromSoftInput(bytes: ByteArray) {
        if (bytes.isEmpty()) return
        viewModel.sendRawBytes(bytes)
        if (ctrlActive || altMode == AltButtonMode.Alt) {
            ctrlActive = false
            if (altMode == AltButtonMode.Alt) altMode = AltButtonMode.Off
        }
    }

    fun sendShiftedNavigationKey(sequence: String) {
        val bytes = modifiedNavigationKeyBytes(
            sequence.encodeToByteArray(),
            ctrlActive = ctrlActive,
            altActive = altMode == AltButtonMode.Alt,
            shiftActive = true,
        ) ?: return
        sendBytesFromSoftInput(bytes)
    }

    fun sendFnFunctionKey(index: Int) {
        functionKeyBytes(
            index,
            ctrlActive = fnModifierMode.ctrlActive,
            altActive = fnModifierMode.altActive,
            shiftActive = fnShiftActive,
        )?.let(viewModel::sendRawBytes)
    }

    fun handleHardwareKey(native: AndroidKeyEvent): Boolean {
        if (native.action != AndroidKeyEvent.ACTION_DOWN) return false
        if (!native.isFromSource(InputDevice.SOURCE_KEYBOARD)) return false

        val ctrlActiveNative = native.metaState and AndroidKeyEvent.META_CTRL_ON != 0
        val altActiveNative = native.metaState and AndroidKeyEvent.META_ALT_ON != 0
        val effectiveCtrl = ctrlActiveNative || ctrlActive
        val effectiveAlt = altActiveNative || altMode == AltButtonMode.Alt

        fun consumeOnScreenModifiers() {
            ctrlActive = false
            if (altMode == AltButtonMode.Alt) altMode = AltButtonMode.Off
        }

        val shiftActiveNative = native.metaState and AndroidKeyEvent.META_SHIFT_ON != 0
        val directBytes = hardwareKeyBytes(
            keyCode = native.keyCode,
            ctrlActive = effectiveCtrl,
            altActive = effectiveAlt,
            shiftActive = shiftActiveNative,
        )
        if (directBytes != null) {
            viewModel.sendRawBytes(directBytes)
            consumeOnScreenModifiers()
            return true
        }

        val unicode = native.unicodeChar
        if (unicode != 0) {
            val text = String(Character.toChars(unicode))
            if (ctrlActiveNative || altActiveNative) {
                val bytes = buildModifiedBytes(text, effectiveCtrl, effectiveAlt)
                if (bytes.isNotEmpty()) {
                    viewModel.sendRawBytes(bytes)
                }
                consumeOnScreenModifiers()
            } else {
                sendTextFromSoftInput(text)
            }
            return true
        }

        if (effectiveCtrl || effectiveAlt || altMode == AltButtonMode.Fn) {
            val mapped = keyCodeToAscii(native.keyCode)
            if (mapped != null) {
                if (altMode == AltButtonMode.Fn && !effectiveCtrl && !effectiveAlt) {
                    sendTextFromSoftInput(mapped.toString())
                } else {
                    val bytes = buildModifiedBytes(mapped.toString(), effectiveCtrl, effectiveAlt)
                    if (bytes.isNotEmpty()) viewModel.sendRawBytes(bytes)
                    consumeOnScreenModifiers()
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
                        onOpenSettings = onOpenSettings,
                        wallInactivityEnabled = state.wallInactivityEnabled,
                        wallInactivityLabel = state.wallInactivityLabel,
                        wallInactivityAvailable = !state.activeSessionId.isNullOrBlank(),
                        onToggleWallInactivity = { viewModel.toggleWallInactivity() },
                        headlessResizeAvailable = !state.activeSessionId.isNullOrBlank(),
                        headlessResizeEnabled = state.activeSessionHeadless,
                        onResizeHeadlessNow = { viewModel.sendHeadlessResizeNow() },
                        onReload = { viewModel.manualRefresh() },
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
                    viewportCacheIdentity = viewportCacheIdentity,
                    palette = palette,
                    fitToViewWidth = true,
                    screenPadding = screenPadding,
                    isCompact = isCompact,
                    isLandscape = true,
                    imeVisible = imeVisible,
                    waitForImeInsetBeforeTerminalLayout = waitingForRestoredImeInset,
                    onHardwareKey = ::handleHardwareKey,
                    ctrlActive = ctrlActive,
                    altMode = altMode,
                    fnShiftActive = fnShiftActive,
                    fnModifierMode = fnModifierMode,
                    selectionMode = selectionMode,
                    onGridViewReady = { activeGridView = it },
                    onToggleCtrl = {
                        if (altMode == AltButtonMode.Fn) fnModifierMode = fnModifierMode.next()
                        else ctrlActive = !ctrlActive
                    },
                    onToggleAlt = {
                        altMode = altMode.next()
                        fnModifierMode = FnModifierMode.Off
                        if (altMode == AltButtonMode.Fn) ctrlActive = false
                        else fnShiftActive = false
                    },
                    onToggleFnShift = { fnShiftActive = !fnShiftActive },
                    onSendKey = ::sendTextFromSoftInput,
                    onSendShiftedKey = ::sendShiftedNavigationKey,
                    onSendFunctionKey = ::sendFnFunctionKey,
                    onSendBytes = ::sendBytesFromSoftInput,
                    onUserDismissIme = handleUserDismissIme,
                    onInputReady = { requestFocus ->
                        requestInputFocus = requestFocus
                        inputReadyNonce += 1
                    },
                    onInputBlurReady = { requestBlur ->
                        requestInputBlur = requestBlur
                    },
                    focusInput = focusInput,
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
                    onOpenSettings = onOpenSettings,
                    wallInactivityEnabled = state.wallInactivityEnabled,
                    wallInactivityLabel = state.wallInactivityLabel,
                    wallInactivityAvailable = !state.activeSessionId.isNullOrBlank(),
                    onToggleWallInactivity = { viewModel.toggleWallInactivity() },
                    headlessResizeAvailable = !state.activeSessionId.isNullOrBlank(),
                    headlessResizeEnabled = state.activeSessionHeadless,
                    onResizeHeadlessNow = { viewModel.sendHeadlessResizeNow() },
                    onReload = { viewModel.manualRefresh() },
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
                }
                TerminalPanel(
                    state = state,
                    viewModel = viewModel,
                    viewportCache = viewportCache,
                    viewportCacheIdentity = viewportCacheIdentity,
                    palette = palette,
                    fitToViewWidth = true,
                    screenPadding = screenPadding,
                    isCompact = isCompact,
                    isLandscape = false,
                    imeVisible = imeVisible,
                    waitForImeInsetBeforeTerminalLayout = waitingForRestoredImeInset,
                    onHardwareKey = ::handleHardwareKey,
                    ctrlActive = ctrlActive,
                    altMode = altMode,
                    fnShiftActive = fnShiftActive,
                    fnModifierMode = fnModifierMode,
                    selectionMode = selectionMode,
                    onGridViewReady = { activeGridView = it },
                    onToggleCtrl = {
                        if (altMode == AltButtonMode.Fn) fnModifierMode = fnModifierMode.next()
                        else ctrlActive = !ctrlActive
                    },
                    onToggleAlt = {
                        altMode = altMode.next()
                        fnModifierMode = FnModifierMode.Off
                        if (altMode == AltButtonMode.Fn) ctrlActive = false
                        else fnShiftActive = false
                    },
                    onToggleFnShift = { fnShiftActive = !fnShiftActive },
                    onSendKey = ::sendTextFromSoftInput,
                    onSendShiftedKey = ::sendShiftedNavigationKey,
                    onSendFunctionKey = ::sendFnFunctionKey,
                    onSendBytes = ::sendBytesFromSoftInput,
                    onUserDismissIme = handleUserDismissIme,
                    onInputReady = { requestFocus ->
                        requestInputFocus = requestFocus
                        inputReadyNonce += 1
                    },
                    onInputBlurReady = { requestBlur ->
                        requestInputBlur = requestBlur
                    },
                    focusInput = focusInput,
                    showStatusOverlay = true,
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
    viewportCacheIdentity: String?,
    palette: TerminalPalette,
    fitToViewWidth: Boolean,
    screenPadding: Dp,
    isCompact: Boolean,
    isLandscape: Boolean,
    imeVisible: Boolean,
    waitForImeInsetBeforeTerminalLayout: Boolean,
    onHardwareKey: (AndroidKeyEvent) -> Boolean,
    ctrlActive: Boolean,
    altMode: AltButtonMode,
    fnShiftActive: Boolean,
    fnModifierMode: FnModifierMode,
    selectionMode: Boolean,
    onGridViewReady: (TerminalGridView) -> Unit,
    onToggleCtrl: () -> Unit,
    onToggleAlt: () -> Unit,
    onToggleFnShift: () -> Unit,
    onSendKey: (String) -> Unit,
    onSendShiftedKey: (String) -> Unit,
    onSendFunctionKey: (Int) -> Unit,
    onSendBytes: (ByteArray) -> Unit,
    onUserDismissIme: () -> Unit,
    onInputReady: ((() -> Unit) -> Unit),
    onInputBlurReady: ((() -> Unit) -> Unit),
    focusInput: () -> Unit,
    showStatusOverlay: Boolean,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .clipToBounds()
            .testTag(TestTags.TerminalFocus),
    ) {
        var restoredSessionId by remember { mutableStateOf<String?>(null) }
        var restoredView by remember { mutableStateOf<TerminalGridView?>(null) }
        var lifecycleRestoreNonce by remember { mutableStateOf(0) }
        var restoredLifecycleNonce by remember { mutableStateOf(0) }
        val sessionId = state.activeSessionId
        val viewportCacheKey = terminalViewportCacheKey(viewportCacheIdentity, sessionId)?.let { key ->
            "$key:${state.terminalConnectionEpoch}"
        }
        val defaultLiveZoom = abs(state.zoomFactor - DefaultTerminalZoom) < 0.001f
        val shouldDelayViewportRestore = state.sessionSyncing && defaultLiveZoom && state.scrollbackOffsetRows == 0
        val lifecycleOwner = LocalLifecycleOwner.current
        Box(
            modifier = Modifier
                .fillMaxWidth()
                .weight(1f)
                .clipToBounds(),
        ) {
            val hostCols = state.activeSnapshot?.cols ?: 0
            val hostRows = state.activeSnapshot?.let { snapshot ->
                (snapshot.rows - state.scrollbackOffsetRows).coerceAtLeast(0)
            } ?: 0
            key(viewportCacheKey) {
                var keyedTerminalGridView by remember { mutableStateOf<TerminalGridView?>(null) }
                DisposableEffect(lifecycleOwner, keyedTerminalGridView, viewportCacheKey) {
                    val disposableViewportKey = viewportCacheKey
                    val disposableView = keyedTerminalGridView
                    val observer = LifecycleEventObserver { _, event ->
                        val activeViewportKey = viewportCacheKey ?: return@LifecycleEventObserver
                        val view = keyedTerminalGridView
                        when (event) {
                            Lifecycle.Event.ON_PAUSE -> {
                                if (view != null) {
                                    viewportCache[activeViewportKey] = view.captureViewportState()
                                }
                            }
                            Lifecycle.Event.ON_START -> {
                                lifecycleRestoreNonce += 1
                            }
                            else -> Unit
                        }
                    }
                    lifecycleOwner.lifecycle.addObserver(observer)
                    onDispose {
                        if (
                            !disposableViewportKey.isNullOrBlank() &&
                            disposableView != null &&
                            lifecycleOwner.lifecycle.currentState.isAtLeast(Lifecycle.State.RESUMED)
                        ) {
                            viewportCache[disposableViewportKey] = disposableView.captureViewportState()
                        }
                        lifecycleOwner.lifecycle.removeObserver(observer)
                    }
                }
                AndroidView(
                    factory = { context ->
                        TerminalGridView(context).apply {
                            keyedTerminalGridView = this
                            tag = "terminal_view"
                            setOnZoomChanged { value ->
                                viewModel.updateZoomFactor(value)
                            }
                            setOnTap { focusInput() }
                            setOnOpenLink { url ->
                                TerminalLinkOpener.open(context, url)
                            }
                            setOnScrollback { deltaRows ->
                                viewModel.adjustScrollback(deltaRows)
                            }
                        }
                    },
                    update = { view ->
                        keyedTerminalGridView = view
                        onGridViewReady(view)
                        view.setSelectionMode(selectionMode)
                        viewportCacheKey?.let { activeViewportKey ->
                            val alreadyRestored =
                                restoredSessionId == activeViewportKey &&
                                    restoredLifecycleNonce == lifecycleRestoreNonce &&
                                    restoredView === view
                            if (!alreadyRestored && !shouldDelayViewportRestore) {
                                viewportCache[activeViewportKey]?.let { cachedViewport ->
                                    view.scheduleViewportRestore(cachedViewport)
                                    restoredSessionId = activeViewportKey
                                    restoredView = view
                                    restoredLifecycleNonce = lifecycleRestoreNonce
                                }
                            }
                        }
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
                            isLoading = state.sessionSyncing ||
                                state.connectionState == ConnectionState.Connecting ||
                                waitForImeInsetBeforeTerminalLayout,
                            followOnReadEnabled = state.followOnReadEnabled,
                            localInputNonce = state.localInputNonce,
                        )
                        view.applyScheduledViewportRestoreIfReady()
                        view.setOnViewSizeChanged { cols, rows ->
                            if (cols <= 0 || rows <= 0) return@setOnViewSizeChanged
                            viewModel.updateTerminalSize(cols, rows)
                        }
                    },
                    modifier = Modifier
                        .fillMaxSize()
                        .clipToBounds()
                        .testTag(TestTags.TerminalList),
                )
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
                            onSendKey("\r")
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
                        onSendKey("\r")
                    }
                    view.setOnHardwareKey(onHardwareKey)
                    view.setOnUserDismissIme(onUserDismissIme)
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
                    .clipToBounds()
                    .testTag(TestTags.TerminalQuickKeys)
                    .navigationBarsPadding()
                    .padding(horizontal = screenPadding, vertical = verticalPadding),
            ) {
                QuickKeysRow(
                    compact = isCompact,
                    dense = isLandscape,
                    singleRow = isLandscape,
                    ctrlActive = ctrlActive,
                    altMode = altMode,
                    fnShiftActive = fnShiftActive,
                    fnModifierMode = fnModifierMode,
                    onToggleCtrl = onToggleCtrl,
                    onToggleAlt = onToggleAlt,
                    onToggleFnShift = onToggleFnShift,
                    onSendKey = { key ->
                        focusInput()
                        onSendKey(key)
                    },
                    onSendShiftedKey = { key ->
                        focusInput()
                        onSendShiftedKey(key)
                    },
                    onSendFunctionKey = { index ->
                        focusInput()
                        onSendFunctionKey(index)
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
    altMode: AltButtonMode,
    fnShiftActive: Boolean,
    fnModifierMode: FnModifierMode,
    onToggleCtrl: () -> Unit,
    onToggleAlt: () -> Unit,
    onToggleFnShift: () -> Unit,
    onSendKey: (String) -> Unit,
    onSendShiftedKey: (String) -> Unit,
    onSendFunctionKey: (Int) -> Unit,
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
    fun navigationKey(label: String, sequence: String) = QuickKeySpec(
        label = label,
        onLongClick = { onSendShiftedKey(sequence) },
        onClick = { onSendKey(sequence) },
    )
    fun ctrlKey() = if (altMode == AltButtonMode.Fn) {
        QuickKeySpec(fnModifierMode.label, active = fnModifierMode != FnModifierMode.Off) { onToggleCtrl() }
    } else {
        QuickKeySpec("CTRL", active = ctrlActive) { onToggleCtrl() }
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
                    ctrlKey(),
                    QuickKeySpec(if (altMode == AltButtonMode.Fn) "FN" else "ALT", active = altMode != AltButtonMode.Off) { onToggleAlt() },
                    navigationKey("←", "\u001b[D"),
                    navigationKey("↓", "\u001b[B"),
                    navigationKey("↑", "\u001b[A"),
                    navigationKey("→", "\u001b[C"),
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
                    if (altMode == AltButtonMode.Fn) QuickKeySpec("SHFT", active = fnShiftActive) { onToggleFnShift() }
                    else QuickKeySpec("/") { onSendKey("/") },
                    QuickKeySpec("-") { onSendKey("-") },
                    navigationKey("HOME", "\u001b[H"),
                    navigationKey("↑", "\u001b[A"),
                    navigationKey("END", "\u001b[F"),
                    if (altMode == AltButtonMode.Fn) QuickKeySpec("F11") { onSendFunctionKey(10) }
                    else navigationKey("PGUP", "\u001b[5~"),
                ),
                height = keyHeight,
                padding = keyPadding,
                textStyle = keyTextStyle,
                spacing = keySpacing,
            )
            QuickKeyRow(
                keys = listOf(
                    QuickKeySpec("TAB") { onSendKey("\t") },
                    ctrlKey(),
                    QuickKeySpec(if (altMode == AltButtonMode.Fn) "FN" else "ALT", active = altMode != AltButtonMode.Off) { onToggleAlt() },
                    navigationKey("←", "\u001b[D"),
                    navigationKey("↓", "\u001b[B"),
                    navigationKey("→", "\u001b[C"),
                    if (altMode == AltButtonMode.Fn) QuickKeySpec("F12") { onSendFunctionKey(11) }
                    else navigationKey("PGDN", "\u001b[6~"),
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
                enabled = key.enabled,
                height = height,
                padding = padding,
                textStyle = textStyle,
                modifier = Modifier.weight(1f),
                onClick = key.onClick,
                onLongClick = key.onLongClick,
            )
        }
    }
}

@Composable
private fun QuickKey(
    label: String,
    active: Boolean,
    enabled: Boolean,
    height: androidx.compose.ui.unit.Dp,
    padding: androidx.compose.ui.unit.Dp,
    textStyle: TextStyle,
    modifier: Modifier,
    onClick: () -> Unit,
    onLongClick: (() -> Unit)?,
) {
    val currentOnClick by rememberUpdatedState(onClick)
    val currentOnLongClick by rememberUpdatedState(onLongClick)
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

    val interactionModifier = if (onLongClick == null) {
        Modifier.clickable(enabled = enabled, onClick = onClick)
    } else {
        Modifier
            .semantics {
                role = Role.Button
                onClick { onClick(); true }
                onLongClick { onLongClick(); true }
            }
            .pointerInput(enabled) {
                if (!enabled) return@pointerInput
                awaitEachGesture {
                    awaitFirstDown()
                    val completed = withTimeoutOrNull(2_000L) {
                        Pair(waitForUpOrCancellation(), true)
                    }
                    when {
                        completed == null -> {
                            currentOnLongClick?.invoke()
                            waitForUpOrCancellation()
                        }
                        completed.first != null -> currentOnClick()
                    }
                }
            }
    }

    Surface(
        modifier = modifier
            .height(height)
            .then(interactionModifier),
        shape = shape,
        color = containerColor,
        border = BorderStroke(1.dp, borderColor),
    ) {
        Row(
            modifier = Modifier.padding(horizontal = padding),
            horizontalArrangement = Arrangement.Center,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(text = label, style = textStyle, color = if (enabled) Color.Unspecified else MaterialTheme.colorScheme.onSurface.copy(alpha = 0.35f), maxLines = 1)
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
    val enabled: Boolean = true,
    val onLongClick: (() -> Unit)? = null,
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
    private var onUserDismissImeCallback: () -> Unit = {}
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
                onUserDismissImeCallback()
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

    fun setOnUserDismissIme(listener: () -> Unit) {
        onUserDismissImeCallback = listener
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
