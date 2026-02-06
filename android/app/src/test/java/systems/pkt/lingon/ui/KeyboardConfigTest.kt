package systems.pkt.lingon.ui

import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Test

class KeyboardConfigTest {
    @Test
    fun loginPasswordOptionsUsePasswordKeyboard() {
        val options = loginPasswordKeyboardOptions()

        assertEquals(KeyboardType.Password, options.keyboardType)
        assertEquals(false, options.autoCorrectEnabled)
        assertEquals(KeyboardCapitalization.None, options.capitalization)
        assertEquals(ImeAction.Done, options.imeAction)
    }

    @Test
    fun terminalOptionsPreferVoiceCompatibleNoSuggestions() {
        val options = terminalInputKeyboardOptions()

        assertEquals(KeyboardType.Text, options.keyboardType)
        assertEquals(false, options.autoCorrectEnabled)
        assertEquals(KeyboardCapitalization.None, options.capitalization)
        assertEquals(ImeAction.Done, options.imeAction)
        val imeOptions = options.platformImeOptions
        assertNotNull(imeOptions)
        assertEquals(
            "com.google.android.inputmethod.latin.noSuggestions=true,com.google.android.inputmethod.latin.noPersonalizedLearning=true",
            imeOptions?.privateImeOptions,
        )
    }
}
