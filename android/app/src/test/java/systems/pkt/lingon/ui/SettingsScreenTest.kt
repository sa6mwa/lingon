package systems.pkt.lingon.ui

import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import systems.pkt.lingon.ui.theme.JetBrainsMono

class SettingsScreenTest {
    @Test
    fun settingsTypographyUsesPlatformDefaultFontFamily() {
        val typography = settingsSystemTypography()

        assertNotEquals(JetBrainsMono, typography.bodyLarge.fontFamily)
        assertNotEquals(JetBrainsMono, typography.titleMedium.fontFamily)
        assertNotEquals(JetBrainsMono, typography.labelLarge.fontFamily)
    }

    @Test
    fun settingsLayoutUsesSystemSettingsScale() {
        assertTrue(SettingsRowMinHeightDp >= 72)
        assertTrue(SettingsIconContainerSizeDp >= 40)
        assertTrue(SettingsGroupCornerRadiusDp >= 20)
        assertTrue(SettingsHorizontalPaddingDp >= 20)
    }
}
