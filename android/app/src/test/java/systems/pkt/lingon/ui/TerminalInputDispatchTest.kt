package systems.pkt.lingon.ui

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
}
