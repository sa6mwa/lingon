package systems.pkt.lingon.ui

import org.junit.Assert.assertEquals
import org.junit.Test

class TerminalImeFocusPolicyTest {
    @Test
    fun automaticFocusWaitsUntilTerminalSnapshotIsReady() {
        val action = decideTerminalImeFocusAction(
            TerminalImeFocusInput(
                terminalReady = false,
                restoreTerminalImeOnLifecycleStart = null,
                imeVisible = false,
                suppressHiddenCapture = false,
            ),
        )

        assertEquals(TerminalImeFocusAction.Ignore, action)
    }

    @Test
    fun visibleRestoreWaitsUntilTerminalSnapshotIsReady() {
        val action = decideTerminalImeFocusAction(
            TerminalImeFocusInput(
                terminalReady = false,
                restoreTerminalImeOnLifecycleStart = true,
                imeVisible = false,
                suppressHiddenCapture = true,
            ),
        )

        assertEquals(TerminalImeFocusAction.Ignore, action)
    }

    @Test
    fun savedHiddenPreferenceBlursEvenBeforeTerminalReady() {
        val action = decideTerminalImeFocusAction(
            TerminalImeFocusInput(
                terminalReady = false,
                restoreTerminalImeOnLifecycleStart = false,
                imeVisible = false,
                suppressHiddenCapture = false,
            ),
        )

        assertEquals(TerminalImeFocusAction.Blur, action)
    }

    @Test
    fun readyTerminalFocusesWhenNoPreferenceExists() {
        val action = decideTerminalImeFocusAction(
            TerminalImeFocusInput(
                terminalReady = true,
                restoreTerminalImeOnLifecycleStart = null,
                imeVisible = false,
                suppressHiddenCapture = false,
            ),
        )

        assertEquals(TerminalImeFocusAction.Focus, action)
    }

    @Test
    fun readyTerminalMarksVisibleRestoreInProgressUntilImeInsetArrives() {
        val action = decideTerminalImeFocusAction(
            TerminalImeFocusInput(
                terminalReady = true,
                restoreTerminalImeOnLifecycleStart = true,
                imeVisible = false,
                suppressHiddenCapture = true,
            ),
        )

        assertEquals(TerminalImeFocusAction.MarkRestoreInProgress, action)
    }

    @Test
    fun readyTerminalRecordsAlreadyVisibleIme() {
        val action = decideTerminalImeFocusAction(
            TerminalImeFocusInput(
                terminalReady = true,
                restoreTerminalImeOnLifecycleStart = true,
                imeVisible = true,
                suppressHiddenCapture = false,
            ),
        )

        assertEquals(TerminalImeFocusAction.RecordVisible, action)
    }
}
