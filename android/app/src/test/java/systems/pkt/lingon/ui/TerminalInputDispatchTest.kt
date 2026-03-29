package systems.pkt.lingon.ui

import android.view.KeyEvent as AndroidKeyEvent
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class TerminalInputDispatchTest {
    @Test
    fun dispatchWithoutModifiersSendsPlainText() {
        val out = dispatchSoftInput(payload = "hello", ctrlActive = false, altActive = false)

        assertEquals("hello", out.text)
        assertNull(out.bytes)
        assertEquals(false, out.nextCtrlActive)
        assertEquals(false, out.nextAltActive)
    }

    @Test
    fun dispatchWithCtrlResetsCtrlAfterNextKey() {
        val out = dispatchSoftInput(payload = "d", ctrlActive = true, altActive = false)

        assertNull(out.text)
        assertArrayEquals(byteArrayOf(0x04), out.bytes)
        assertEquals(false, out.nextCtrlActive)
        assertEquals(false, out.nextAltActive)
    }

    @Test
    fun dispatchWithAltResetsAltAfterNextKey() {
        val out = dispatchSoftInput(payload = "d", ctrlActive = false, altActive = true)

        assertNull(out.text)
        assertArrayEquals(byteArrayOf(0x1b, 'd'.code.toByte()), out.bytes)
        assertEquals(false, out.nextCtrlActive)
        assertEquals(false, out.nextAltActive)
    }

    @Test
    fun dispatchEmptyPayloadKeepsModifierState() {
        val out = dispatchSoftInput(payload = "", ctrlActive = true, altActive = false)

        assertNull(out.text)
        assertNull(out.bytes)
        assertEquals(true, out.nextCtrlActive)
        assertEquals(false, out.nextAltActive)
    }

    @Test
    fun dispatchEmptyPayloadKeepsAltState() {
        val out = dispatchSoftInput(payload = "", ctrlActive = false, altActive = true)

        assertNull(out.text)
        assertNull(out.bytes)
        assertEquals(false, out.nextCtrlActive)
        assertEquals(true, out.nextAltActive)
    }

    @Test
    fun dispatchWithCtrlAltResetsBothAfterNextKey() {
        val out = dispatchSoftInput(payload = "d", ctrlActive = true, altActive = true)

        assertNull(out.text)
        assertArrayEquals(byteArrayOf(0x1b, 0x04), out.bytes)
        assertEquals(false, out.nextCtrlActive)
        assertEquals(false, out.nextAltActive)
    }

    @Test
    fun dispatchEnterWithAltSendsPlainCarriageReturn() {
        val out = dispatchSoftInput(payload = "\n", ctrlActive = false, altActive = true)

        assertNull(out.text)
        assertArrayEquals(byteArrayOf('\r'.code.toByte()), out.bytes)
        assertEquals(false, out.nextCtrlActive)
        assertEquals(false, out.nextAltActive)
    }

    @Test
    fun dispatchCarriageReturnWithAltSendsPlainCarriageReturn() {
        val out = dispatchSoftInput(payload = "\r", ctrlActive = false, altActive = true)

        assertNull(out.text)
        assertArrayEquals(byteArrayOf('\r'.code.toByte()), out.bytes)
        assertEquals(false, out.nextCtrlActive)
        assertEquals(false, out.nextAltActive)
    }

    @Test
    fun translateAppCursorKeysLeavesCsiArrowsWhenInactive() {
        val out = translateAppCursorKeys("\u001b[B".encodeToByteArray(), appCursorActive = false)

        assertArrayEquals("\u001b[B".encodeToByteArray(), out)
    }

    @Test
    fun translateAppCursorKeysConvertsArrowsAndHomeEndWhenActive() {
        assertArrayEquals("\u001bOA".encodeToByteArray(), translateAppCursorKeys("\u001b[A".encodeToByteArray(), true))
        assertArrayEquals("\u001bOB".encodeToByteArray(), translateAppCursorKeys("\u001b[B".encodeToByteArray(), true))
        assertArrayEquals("\u001bOC".encodeToByteArray(), translateAppCursorKeys("\u001b[C".encodeToByteArray(), true))
        assertArrayEquals("\u001bOD".encodeToByteArray(), translateAppCursorKeys("\u001b[D".encodeToByteArray(), true))
        assertArrayEquals("\u001bOH".encodeToByteArray(), translateAppCursorKeys("\u001b[H".encodeToByteArray(), true))
        assertArrayEquals("\u001bOF".encodeToByteArray(), translateAppCursorKeys("\u001b[F".encodeToByteArray(), true))
    }

    @Test
    fun translateAppCursorKeysLeavesModifiedCsiUnchanged() {
        val out = translateAppCursorKeys("\u001b[1;2B".encodeToByteArray(), appCursorActive = true)

        assertArrayEquals("\u001b[1;2B".encodeToByteArray(), out)
    }

    @Test
    fun hardwareKeyBytesMapsArrowAndHomeEndKeys() {
        assertArrayEquals("\u001b[A".encodeToByteArray(), hardwareKeyBytes(AndroidKeyEvent.KEYCODE_DPAD_UP))
        assertArrayEquals("\u001b[B".encodeToByteArray(), hardwareKeyBytes(AndroidKeyEvent.KEYCODE_DPAD_DOWN))
        assertArrayEquals("\u001b[D".encodeToByteArray(), hardwareKeyBytes(AndroidKeyEvent.KEYCODE_DPAD_LEFT))
        assertArrayEquals("\u001b[C".encodeToByteArray(), hardwareKeyBytes(AndroidKeyEvent.KEYCODE_DPAD_RIGHT))
        assertArrayEquals("\u001b[H".encodeToByteArray(), hardwareKeyBytes(AndroidKeyEvent.KEYCODE_MOVE_HOME))
        assertArrayEquals("\u001b[F".encodeToByteArray(), hardwareKeyBytes(AndroidKeyEvent.KEYCODE_MOVE_END))
    }

    @Test
    fun imeDeleteSurroundingTextIsNotForwardedAsTerminalBackspace() {
        assertEquals(false, shouldForwardImeDeleteSurroundingTextAsBackspace(leftLength = 1, rightLength = 0))
        assertEquals(false, shouldForwardImeDeleteSurroundingTextAsBackspace(leftLength = 3, rightLength = 0))
        assertEquals(false, shouldForwardImeDeleteSurroundingTextAsBackspace(leftLength = 1, rightLength = 1))
    }
}
