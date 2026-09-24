package systems.pkt.lingon.ui

import android.view.KeyEvent as AndroidKeyEvent
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class TerminalInputDispatchTest {
    @Test
    fun altButtonCyclesThroughAltAndPersistentFn() {
        assertEquals(AltButtonMode.Alt, AltButtonMode.Off.next())
        assertEquals(AltButtonMode.Fn, AltButtonMode.Alt.next())
        assertEquals(AltButtonMode.Off, AltButtonMode.Fn.next())
    }

    @Test
    fun fnCommandsAreCaseInsensitiveAndOtherKeysStayLiteral() {
        assertEquals(FnInput.ToggleSelection, dispatchFnKey('s'))
        assertEquals(FnInput.ToggleSelection, dispatchFnKey('S'))
        assertEquals(FnInput.Copy, dispatchFnKey('c'))
        assertEquals(FnInput.Copy, dispatchFnKey('C'))
        assertEquals(FnInput.Paste, dispatchFnKey('p'))
        assertEquals(FnInput.Paste, dispatchFnKey('P'))
        assertEquals(FnInput.Literal("x"), dispatchFnKey('x'))
        assertArrayEquals("\u001bOP".encodeToByteArray(), (dispatchFnKey('1') as FnInput.FunctionKey).bytes)
    }

    @Test
    fun fnNumberAndSymbolKeysEncodeAllTwelveFunctionKeys() {
        val keys = "1234567890@#"
        val expected = listOf(
            "\u001bOP", "\u001bOQ", "\u001bOR", "\u001bOS",
            "\u001b[15~", "\u001b[17~", "\u001b[18~", "\u001b[19~",
            "\u001b[20~", "\u001b[21~", "\u001b[23~", "\u001b[24~",
        )
        keys.forEachIndexed { index, key ->
            assertArrayEquals(expected[index].encodeToByteArray(), fnFunctionKeyBytes(key))
        }
        assertNull(fnFunctionKeyBytes('x'))
    }

    @Test
    fun physicalFunctionKeysPreserveCtrlAltChords() {
        assertArrayEquals("\u001bOP".encodeToByteArray(), hardwareKeyBytes(AndroidKeyEvent.KEYCODE_F1))
        assertArrayEquals("\u001b[1;7P".encodeToByteArray(), hardwareKeyBytes(AndroidKeyEvent.KEYCODE_F1, ctrlActive = true, altActive = true))
        assertArrayEquals("\u001b[24;7~".encodeToByteArray(), hardwareKeyBytes(AndroidKeyEvent.KEYCODE_F12, ctrlActive = true, altActive = true))
    }

    @Test
    fun allTouchArrowChordsUseCsiModifierEncoding() {
        val arrows = mapOf('A' to "\u001b[A", 'B' to "\u001b[B", 'C' to "\u001b[C", 'D' to "\u001b[D")
        val chords = listOf(Triple(true, false, 5), Triple(false, true, 3), Triple(true, true, 7))
        arrows.forEach { (final, sequence) ->
            chords.forEach { (ctrl, alt, modifier) ->
                val out = dispatchSoftInput(sequence, ctrlActive = ctrl, altActive = alt)
                assertArrayEquals("\u001b[1;${modifier}${final}".encodeToByteArray(), out.bytes)
            }
        }
    }

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
    fun dispatchWithCtrlAltTreatsUppercaseLettersAsTheSameControlChord() {
        val out = dispatchSoftInput(payload = "D", ctrlActive = true, altActive = true)

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
    fun buildModifiedBytesUsesSingleCsiSequenceForModifiedCursorKeys() {
        assertArrayEquals(
            "\u001b[1;3A".encodeToByteArray(),
            buildModifiedBytes("\u001b[A", ctrlActive = false, altActive = true),
        )
        assertArrayEquals(
            "\u001b[1;7B".encodeToByteArray(),
            buildModifiedBytes("\u001b[B", ctrlActive = true, altActive = true),
        )
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
    fun hardwareKeyBytesMapsModifiedCursorKeys() {
        assertArrayEquals(
            "\u001b[1;3A".encodeToByteArray(),
            hardwareKeyBytes(AndroidKeyEvent.KEYCODE_DPAD_UP, altActive = true),
        )
        assertArrayEquals(
            "\u001b[1;7B".encodeToByteArray(),
            hardwareKeyBytes(AndroidKeyEvent.KEYCODE_DPAD_DOWN, ctrlActive = true, altActive = true),
        )
        assertArrayEquals(
            "\u001b[1;8B".encodeToByteArray(),
            hardwareKeyBytes(
                AndroidKeyEvent.KEYCODE_DPAD_DOWN,
                ctrlActive = true,
                altActive = true,
                shiftActive = true,
            ),
        )
    }

    @Test
    fun imeDeleteSurroundingTextForwardsPlainBackspaceOnly() {
        assertEquals(true, shouldForwardImeDeleteSurroundingTextAsBackspace(leftLength = 1, rightLength = 0))
        assertEquals(true, shouldForwardImeDeleteSurroundingTextAsBackspace(leftLength = 3, rightLength = 0))
        assertEquals(false, shouldForwardImeDeleteSurroundingTextAsBackspace(leftLength = 1, rightLength = 1))
        assertEquals(false, shouldForwardImeDeleteSurroundingTextAsBackspace(leftLength = 0, rightLength = 1))
    }
}
