package systems.pkt.lingon.ui.dialogs

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RenameSessionDialogTest {
    @Test
    fun validatesUnicodeCodePointsAndRejectsInvisibleControls() {
        assertTrue(isValidSessionName(" 日本語 🦊 "))
        assertTrue(isValidSessionName("🦊".repeat(80)))
        assertFalse(isValidSessionName("🦊".repeat(81)))
        assertFalse(isValidSessionName(" \t "))
        assertFalse(isValidSessionName("name\u202E"))
        assertFalse(isValidSessionName("name\uDB40\uDC01")) // Supplementary-plane format character.
        assertFalse(isValidSessionName("line\nname"))
    }
}
