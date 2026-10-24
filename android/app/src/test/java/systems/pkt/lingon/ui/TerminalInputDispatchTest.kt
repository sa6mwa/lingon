package systems.pkt.lingon.ui

import android.view.KeyEvent as AndroidKeyEvent
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class TerminalInputDispatchTest {
    @Test
    fun fnLiteralRunsPreserveUnicodeAndShortcutOrder() {
        val actions = dispatchFnInput("😀𠮷e\u0301C1åV🌍", shiftActive = true, ctrlActive = true, altActive = true)
        assertEquals(6, actions.size)
        assertEquals(FnInput.Literal("😀𠮷e\u0301"), actions[0])
        assertEquals(FnInput.Copy, actions[1])
        assertArrayEquals("\u001b[1;8P".encodeToByteArray(), (actions[2] as FnInput.FunctionKey).bytes)
        assertEquals(FnInput.Literal("å"), actions[3])
        assertEquals(FnInput.Paste, actions[4])
        assertEquals(FnInput.Literal("🌍"), actions[5])
        assertEquals(emptyList<FnInput>(), dispatchFnInput(""))
        assertEquals(listOf(FnInput.Literal("a".repeat(4096))), dispatchFnInput("a".repeat(4096)))
    }

    @Test
    fun softModifiersPreserveSupplementaryCodePoints() {
        val text = "😀𠮷"
        assertArrayEquals(text.encodeToByteArray(), buildModifiedBytes(text, ctrlActive = true, altActive = false))
        val withAlt = "\u001b😀\u001b𠮷".encodeToByteArray()
        assertArrayEquals(withAlt, buildModifiedBytes(text, ctrlActive = false, altActive = true))
        assertArrayEquals(withAlt, buildModifiedBytes(text, ctrlActive = true, altActive = true))
    }

    @Test
    fun quickNavigationKeysEncodeShiftAndActiveModifiers() {
        val keys = mapOf(
            "\u001b[A" to "\u001b[1;2A",
            "\u001b[B" to "\u001b[1;2B",
            "\u001b[C" to "\u001b[1;2C",
            "\u001b[D" to "\u001b[1;2D",
            "\u001b[H" to "\u001b[1;2H",
            "\u001b[F" to "\u001b[1;2F",
            "\u001b[5~" to "\u001b[5;2~",
            "\u001b[6~" to "\u001b[6;2~",
        )
        for ((key, shifted) in keys) {
            assertArrayEquals(key.encodeToByteArray(), modifiedNavigationKeyBytes(key.encodeToByteArray(), false, false, false))
            assertArrayEquals(shifted.encodeToByteArray(), modifiedNavigationKeyBytes(key.encodeToByteArray(), false, false, true))
            for ((ctrl, alt, modifier) in listOf(
                Triple(false, true, 4), Triple(true, false, 6), Triple(true, true, 8),
            )) {
                val expected = if (key.endsWith('~')) {
                    key.dropLast(1) + ";$modifier~"
                } else {
                    "\u001b[1;$modifier${key.last()}"
                }
                assertArrayEquals(expected.encodeToByteArray(), modifiedNavigationKeyBytes(key.encodeToByteArray(), ctrl, alt, true))
            }
        }
    }

    @Test
    fun altButtonCyclesThroughAltAndPersistentFn() {
        assertEquals(AltButtonMode.Alt, AltButtonMode.Off.next())
        assertEquals(AltButtonMode.Fn, AltButtonMode.Alt.next())
        assertEquals(AltButtonMode.Off, AltButtonMode.Fn.next())
    }

    @Test
    fun fnModifierButtonCyclesThroughCtrlAltAndBoth() {
        assertEquals(FnModifierMode.Ctrl, FnModifierMode.Off.next())
        assertEquals(FnModifierMode.Alt, FnModifierMode.Ctrl.next())
        assertEquals(FnModifierMode.CtrlAlt, FnModifierMode.Alt.next())
        assertEquals(FnModifierMode.Off, FnModifierMode.CtrlAlt.next())
        assertEquals(false, FnModifierMode.Off.ctrlActive)
        assertEquals(false, FnModifierMode.Off.altActive)
        assertEquals(true, FnModifierMode.Ctrl.ctrlActive)
        assertEquals(false, FnModifierMode.Ctrl.altActive)
        assertEquals(false, FnModifierMode.Alt.ctrlActive)
        assertEquals(true, FnModifierMode.Alt.altActive)
        assertEquals(true, FnModifierMode.CtrlAlt.ctrlActive)
        assertEquals(true, FnModifierMode.CtrlAlt.altActive)
    }

    @Test
    fun fnFunctionKeysCombinePersistentModifierAndShiftStates() {
        val chords = listOf(
            Triple(true, false, 5), Triple(false, true, 3), Triple(true, true, 7),
        )
        for ((ctrl, alt, modifier) in chords) {
            assertArrayEquals("\u001b[1;${modifier}P".encodeToByteArray(), fnFunctionKeyBytes('1', ctrlActive = ctrl, altActive = alt))
            assertArrayEquals("\u001b[21;${modifier}~".encodeToByteArray(), fnFunctionKeyBytes('0', ctrlActive = ctrl, altActive = alt))
            assertArrayEquals("\u001b[24;${modifier}~".encodeToByteArray(), functionKeyBytes(11, ctrlActive = ctrl, altActive = alt))
        }
        assertArrayEquals("\u001b[1;8P".encodeToByteArray(), fnFunctionKeyBytes('1', ctrlActive = true, altActive = true, shiftActive = true))
        assertArrayEquals("\u001b[23;8~".encodeToByteArray(), functionKeyBytes(10, ctrlActive = true, altActive = true, shiftActive = true))
        assertEquals(FnInput.Literal("s"), dispatchFnKey('s', ctrlActive = true, altActive = true))
        assertEquals(FnInput.Copy, dispatchFnKey('c', ctrlActive = true, altActive = true))
        assertEquals(FnInput.Paste, dispatchFnKey('v', ctrlActive = true, altActive = true))
    }

    @Test
    fun fnCommandsAreCaseInsensitiveAndOtherKeysStayLiteral() {
        assertEquals(FnInput.Literal("s"), dispatchFnKey('s'))
        assertEquals(FnInput.Literal("S"), dispatchFnKey('S'))
        assertEquals(FnInput.Copy, dispatchFnKey('c'))
        assertEquals(FnInput.Copy, dispatchFnKey('C'))
        assertEquals(FnInput.Paste, dispatchFnKey('v'))
        assertEquals(FnInput.Paste, dispatchFnKey('V'))
        assertEquals(FnInput.Literal("p"), dispatchFnKey('p'))
        assertEquals(FnInput.Literal("x"), dispatchFnKey('x'))
        assertArrayEquals("\u001bOP".encodeToByteArray(), (dispatchFnKey('1') as FnInput.FunctionKey).bytes)
    }

    @Test
    fun fnNumberKeysEncodeF1ThroughF10AndFnShiftEncodesModifiedFunctionKeys() {
        val keys = "1234567890"
        val expected = listOf(
            "\u001bOP", "\u001bOQ", "\u001bOR", "\u001bOS",
            "\u001b[15~", "\u001b[17~", "\u001b[18~", "\u001b[19~",
            "\u001b[20~", "\u001b[21~",
        )
        keys.forEachIndexed { index, key ->
            assertArrayEquals(expected[index].encodeToByteArray(), fnFunctionKeyBytes(key))
        }
        assertNull(fnFunctionKeyBytes('x'))
        assertNull(fnFunctionKeyBytes('@'))
        assertNull(fnFunctionKeyBytes('#'))
        assertEquals(FnInput.Literal("@"), dispatchFnKey('@'))
        assertEquals(FnInput.Literal("#"), dispatchFnKey('#'))
        assertArrayEquals("\u001b[1;2P".encodeToByteArray(), fnFunctionKeyBytes('1', shiftActive = true))
        assertArrayEquals("\u001b[21;2~".encodeToByteArray(), fnFunctionKeyBytes('0', shiftActive = true))
        assertArrayEquals("\u001b[23;2~".encodeToByteArray(), functionKeyBytes(10, shiftActive = true))
        assertArrayEquals("\u001b[24;2~".encodeToByteArray(), functionKeyBytes(11, shiftActive = true))
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
    fun dispatchEnterWithAltSendsEscapePrefixedCarriageReturn() {
        val out = dispatchSoftInput(payload = "\n", ctrlActive = false, altActive = true)

        assertNull(out.text)
        assertArrayEquals(byteArrayOf(0x1b, '\r'.code.toByte()), out.bytes)
        assertEquals(false, out.nextCtrlActive)
        assertEquals(false, out.nextAltActive)
    }

    @Test
    fun dispatchCarriageReturnWithAltSendsEscapePrefixedCarriageReturn() {
        val out = dispatchSoftInput(payload = "\r", ctrlActive = false, altActive = true)

        assertNull(out.text)
        assertArrayEquals(byteArrayOf(0x1b, '\r'.code.toByte()), out.bytes)
        assertEquals(false, out.nextCtrlActive)
        assertEquals(false, out.nextAltActive)
    }

    @Test
    fun dispatchEnterWithoutAltStaysCarriageReturn() {
        for (ctrl in listOf(false, true)) {
            val out = dispatchSoftInput(payload = "\n", ctrlActive = ctrl, altActive = false)
            assertArrayEquals(byteArrayOf('\r'.code.toByte()), out.bytes)
            assertEquals(false, out.nextCtrlActive)
        }
    }

    @Test
    fun dispatchEnterWithCtrlAndAltSendsEscapePrefixedCarriageReturn() {
        val out = dispatchSoftInput(payload = "\n", ctrlActive = true, altActive = true)

        assertArrayEquals(byteArrayOf(0x1b, '\r'.code.toByte()), out.bytes)
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
