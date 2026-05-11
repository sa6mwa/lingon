package systems.pkt.lingon.ui

internal enum class TerminalImeLifecycleAction {
    Ignore,
    RecordVisible,
    BlurOnly,
    CompleteUserDismissAndBlur,
    RecordHiddenAndBlur,
}

internal data class TerminalImeLifecycleInput(
    val imeVisible: Boolean,
    val captureImeInsetChanges: Boolean,
    val lifecycleResumed: Boolean,
    val restoreTerminalImeOnLifecycleStart: Boolean?,
    val imeRestoreInProgress: Boolean,
    val observedTerminalImeVisible: Boolean,
    val userDismissInProgress: Boolean,
)

internal fun decideTerminalImeLifecycleAction(input: TerminalImeLifecycleInput): TerminalImeLifecycleAction {
    if (!input.captureImeInsetChanges || !input.lifecycleResumed) {
        return TerminalImeLifecycleAction.Ignore
    }
    if (input.userDismissInProgress) {
        return if (input.imeVisible) {
            TerminalImeLifecycleAction.Ignore
        } else {
            TerminalImeLifecycleAction.CompleteUserDismissAndBlur
        }
    }
    if (input.imeVisible) {
        return TerminalImeLifecycleAction.RecordVisible
    }
    if (input.imeRestoreInProgress) {
        return TerminalImeLifecycleAction.Ignore
    }
    return when (input.restoreTerminalImeOnLifecycleStart) {
        true -> TerminalImeLifecycleAction.Ignore
        false -> TerminalImeLifecycleAction.BlurOnly
        null -> {
            if (input.observedTerminalImeVisible) {
                TerminalImeLifecycleAction.RecordHiddenAndBlur
            } else {
                TerminalImeLifecycleAction.Ignore
            }
        }
    }
}
