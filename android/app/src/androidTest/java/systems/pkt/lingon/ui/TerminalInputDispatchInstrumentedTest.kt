package systems.pkt.lingon.ui

import android.view.KeyEvent
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertArrayEquals
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class TerminalInputDispatchInstrumentedTest {
    @Test
    fun modifiedKeysUseTerminalCompatibleEncodings() {
        assertArrayEquals(
            "\u001b[1;3A".encodeToByteArray(),
            hardwareKeyBytes(KeyEvent.KEYCODE_DPAD_UP, altActive = true),
        )
        assertArrayEquals(
            "\u001b[1;7B".encodeToByteArray(),
            hardwareKeyBytes(KeyEvent.KEYCODE_DPAD_DOWN, ctrlActive = true, altActive = true),
        )
        assertArrayEquals(
            "\u001b[1;8B".encodeToByteArray(),
            hardwareKeyBytes(
                KeyEvent.KEYCODE_DPAD_DOWN,
                ctrlActive = true,
                altActive = true,
                shiftActive = true,
            ),
        )
        assertArrayEquals(
            byteArrayOf(0x1b, 0x04),
            buildModifiedBytes("D", ctrlActive = true, altActive = true),
        )
    }
}
