package systems.pkt.lingon.ui.theme

import androidx.compose.material3.Typography
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp
import systems.pkt.lingon.R

val JetBrainsMono = FontFamily(
    Font(R.font.jetbrains_mono_regular, FontWeight.Normal),
    Font(R.font.jetbrains_mono_italic, FontWeight.Normal, FontStyle.Italic),
    Font(R.font.jetbrains_mono_bold, FontWeight.Bold),
    Font(R.font.jetbrains_mono_bold_italic, FontWeight.Bold, FontStyle.Italic),
)

fun lingonTypography(base: Typography): Typography {
    return base.copy(
        displayLarge = base.displayLarge.copy(fontFamily = JetBrainsMono),
        displayMedium = base.displayMedium.copy(fontFamily = JetBrainsMono),
        displaySmall = base.displaySmall.copy(fontFamily = JetBrainsMono),
        headlineLarge = base.headlineLarge.copy(fontFamily = JetBrainsMono),
        headlineMedium = base.headlineMedium.copy(fontFamily = JetBrainsMono),
        headlineSmall = base.headlineSmall.copy(fontFamily = JetBrainsMono),
        titleLarge = base.titleLarge.copy(fontFamily = JetBrainsMono),
        titleMedium = base.titleMedium.copy(
            fontFamily = JetBrainsMono,
            fontWeight = FontWeight.SemiBold,
            fontSize = 16.sp,
            lineHeight = 22.sp,
        ),
        titleSmall = base.titleSmall.copy(fontFamily = JetBrainsMono),
        bodyLarge = base.bodyLarge.copy(fontFamily = JetBrainsMono),
        bodyMedium = base.bodyMedium.copy(
            fontFamily = JetBrainsMono,
            fontWeight = FontWeight.Normal,
            fontSize = 14.sp,
            lineHeight = 20.sp,
        ),
        bodySmall = base.bodySmall.copy(fontFamily = JetBrainsMono),
        labelLarge = base.labelLarge.copy(fontFamily = JetBrainsMono),
        labelMedium = base.labelMedium.copy(fontFamily = JetBrainsMono),
        labelSmall = base.labelSmall.copy(fontFamily = JetBrainsMono),
    )
}
