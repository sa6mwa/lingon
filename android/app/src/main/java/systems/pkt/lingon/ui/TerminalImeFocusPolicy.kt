package systems.pkt.lingon.ui

internal enum class TerminalImeFocusAction {
    Ignore,
    Blur,
    Focus,
    RecordVisible,
    MarkRestoreInProgress,
}

internal data class TerminalImeFocusInput(
    val terminalReady: Boolean,
    val restoreTerminalImeOnLifecycleStart: Boolean?,
    val imeVisible: Boolean,
    val suppressHiddenCapture: Boolean,
)

internal fun decideTerminalImeFocusAction(input: TerminalImeFocusInput): TerminalImeFocusAction {
    if (input.restoreTerminalImeOnLifecycleStart == false) {
        return TerminalImeFocusAction.Blur
    }
    if (!input.terminalReady) {
        return TerminalImeFocusAction.Ignore
    }
    if (input.imeVisible) {
        return TerminalImeFocusAction.RecordVisible
    }
    if (input.suppressHiddenCapture && input.restoreTerminalImeOnLifecycleStart == true) {
        return TerminalImeFocusAction.MarkRestoreInProgress
    }
    return TerminalImeFocusAction.Focus
}
