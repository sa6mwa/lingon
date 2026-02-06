package systems.pkt.lingon.ui.theme

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.graphics.Color

data class LingonExtraColors(
    val panel: Color,
    val border: Color,
    val muted: Color,
    val reasoning: Color,
    val reasoningBold: Color,
    val code: Color,
    val stderr: Color,
    val aboutLink: Color,
    val aboutCopyright: Color,
    val helpArg: Color,
    val inputBg: Color,
)

val LocalLingonExtraColors = staticCompositionLocalOf {
    LingonExtraColors(
        panel = OutrunPanel,
        border = OutrunBorder,
        muted = OutrunMuted,
        reasoning = OutrunReasoning,
        reasoningBold = OutrunReasoningBold,
        code = OutrunCode,
        stderr = OutrunStderr,
        aboutLink = OutrunAccent,
        aboutCopyright = OutrunAboutCopyright,
        helpArg = OutrunHelpArg,
        inputBg = OutrunInputBg,
    )
}

private val OutrunScheme = darkColorScheme(
    primary = OutrunAccent,
    secondary = OutrunReasoning,
    tertiary = OutrunCode,
    background = OutrunBackground,
    surface = OutrunPanel,
    surfaceVariant = OutrunInputBg,
    error = OutrunDanger,
    onPrimary = Color(0xFF0B0F14),
    onSecondary = OutrunText,
    onTertiary = OutrunText,
    onBackground = OutrunText,
    onSurface = OutrunText,
    onError = Color(0xFF0B0F14),
)

private val GruvboxScheme = darkColorScheme(
    primary = GruvboxAccent,
    secondary = GruvboxReasoning,
    tertiary = GruvboxCode,
    background = GruvboxBackground,
    surface = GruvboxPanel,
    surfaceVariant = GruvboxInputBg,
    error = GruvboxDanger,
    onPrimary = Color(0xFF1B1B1B),
    onSecondary = GruvboxText,
    onTertiary = GruvboxText,
    onBackground = GruvboxText,
    onSurface = GruvboxText,
    onError = Color(0xFF1B1B1B),
)

private val TokyoScheme = darkColorScheme(
    primary = TokyoAccent,
    secondary = TokyoReasoning,
    tertiary = TokyoCode,
    background = TokyoBackground,
    surface = TokyoPanel,
    surfaceVariant = TokyoInputBg,
    error = TokyoDanger,
    onPrimary = Color(0xFF0F121B),
    onSecondary = TokyoText,
    onTertiary = TokyoText,
    onBackground = TokyoText,
    onSurface = TokyoText,
    onError = Color(0xFF0F121B),
)

@Composable
fun LingonTheme(themeName: String?, content: @Composable () -> Unit) {
    val extras = when (themeName?.lowercase()) {
        "gruvbox" -> LingonExtraColors(
            panel = GruvboxPanel,
            border = GruvboxBorder,
            muted = GruvboxMuted,
            reasoning = GruvboxReasoning,
            reasoningBold = GruvboxReasoningBold,
            code = GruvboxCode,
            stderr = GruvboxStderr,
            aboutLink = GruvboxAccent,
            aboutCopyright = GruvboxAboutCopyright,
            helpArg = GruvboxHelpArg,
            inputBg = GruvboxInputBg,
        )
        "tokyo-midnight", "tokyo" -> LingonExtraColors(
            panel = TokyoPanel,
            border = TokyoBorder,
            muted = TokyoMuted,
            reasoning = TokyoReasoning,
            reasoningBold = TokyoReasoningBold,
            code = TokyoCode,
            stderr = TokyoStderr,
            aboutLink = TokyoAccent,
            aboutCopyright = TokyoAboutCopyright,
            helpArg = TokyoHelpArg,
            inputBg = TokyoInputBg,
        )
        else -> LingonExtraColors(
            panel = OutrunPanel,
            border = OutrunBorder,
            muted = OutrunMuted,
            reasoning = OutrunReasoning,
            reasoningBold = OutrunReasoningBold,
            code = OutrunCode,
            stderr = OutrunStderr,
            aboutLink = OutrunAccent,
            aboutCopyright = OutrunAboutCopyright,
            helpArg = OutrunHelpArg,
            inputBg = OutrunInputBg,
        )
    }

    val scheme = when (themeName?.lowercase()) {
        "gruvbox" -> GruvboxScheme
        "tokyo-midnight", "tokyo" -> TokyoScheme
        else -> OutrunScheme
    }
    val typography = lingonTypography(MaterialTheme.typography)

    CompositionLocalProvider(LocalLingonExtraColors provides extras) {
        MaterialTheme(
            colorScheme = scheme,
            typography = typography,
            content = content,
        )
    }
}
