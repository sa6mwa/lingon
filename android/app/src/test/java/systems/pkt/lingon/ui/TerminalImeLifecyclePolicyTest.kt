package systems.pkt.lingon.ui

import org.junit.Assert.assertEquals
import org.junit.Test

class TerminalImeLifecyclePolicyTest {
    @Test
    fun platformHiddenInsetAfterVisibleLifecycleRestoreDoesNotPersistHidden() {
        val action = decideTerminalImeLifecycleAction(
            TerminalImeLifecycleInput(
                imeVisible = false,
                captureImeInsetChanges = true,
                lifecycleResumed = true,
                restoreTerminalImeOnLifecycleStart = true,
                imeRestoreInProgress = false,
                observedTerminalImeVisible = true,
                userDismissInProgress = false,
            ),
        )

        assertEquals(TerminalImeLifecycleAction.RequestFocus, action)
    }

    @Test
    fun savedHiddenPreferenceBlursWhenImeInsetIsHidden() {
        val action = decideTerminalImeLifecycleAction(
            TerminalImeLifecycleInput(
                imeVisible = false,
                captureImeInsetChanges = true,
                lifecycleResumed = true,
                restoreTerminalImeOnLifecycleStart = false,
                imeRestoreInProgress = false,
                observedTerminalImeVisible = true,
                userDismissInProgress = false,
            ),
        )

        assertEquals(TerminalImeLifecycleAction.BlurOnly, action)
    }

    @Test
    fun visibleInsetRecordsVisiblePreference() {
        val action = decideTerminalImeLifecycleAction(
            TerminalImeLifecycleInput(
                imeVisible = true,
                captureImeInsetChanges = true,
                lifecycleResumed = true,
                restoreTerminalImeOnLifecycleStart = false,
                imeRestoreInProgress = false,
                observedTerminalImeVisible = false,
                userDismissInProgress = false,
            ),
        )

        assertEquals(TerminalImeLifecycleAction.RecordVisible, action)
    }

    @Test
    fun pausedLifecycleIgnoresHiddenInsets() {
        val action = decideTerminalImeLifecycleAction(
            TerminalImeLifecycleInput(
                imeVisible = false,
                captureImeInsetChanges = false,
                lifecycleResumed = false,
                restoreTerminalImeOnLifecycleStart = true,
                imeRestoreInProgress = false,
                observedTerminalImeVisible = true,
                userDismissInProgress = false,
            ),
        )

        assertEquals(TerminalImeLifecycleAction.Ignore, action)
    }

    @Test
    fun visibleInsetDuringUserDismissDoesNotRecordVisibleAgain() {
        val action = decideTerminalImeLifecycleAction(
            TerminalImeLifecycleInput(
                imeVisible = true,
                captureImeInsetChanges = true,
                lifecycleResumed = true,
                restoreTerminalImeOnLifecycleStart = false,
                imeRestoreInProgress = false,
                observedTerminalImeVisible = true,
                userDismissInProgress = true,
            ),
        )

        assertEquals(TerminalImeLifecycleAction.Ignore, action)
    }

    @Test
    fun hiddenInsetCompletesUserDismiss() {
        val action = decideTerminalImeLifecycleAction(
            TerminalImeLifecycleInput(
                imeVisible = false,
                captureImeInsetChanges = true,
                lifecycleResumed = true,
                restoreTerminalImeOnLifecycleStart = false,
                imeRestoreInProgress = false,
                observedTerminalImeVisible = true,
                userDismissInProgress = true,
            ),
        )

        assertEquals(TerminalImeLifecycleAction.CompleteUserDismissAndBlur, action)
    }
}
