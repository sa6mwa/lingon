package systems.pkt.lingon.ui.terminal

import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.clickable
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.input.nestedscroll.NestedScrollConnection
import androidx.compose.ui.input.nestedscroll.NestedScrollSource
import androidx.compose.ui.input.nestedscroll.nestedScroll
import androidx.compose.ui.geometry.Offset
import kotlinx.coroutines.flow.first
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.platform.testTag
import systems.pkt.lingon.ui.theme.LingonExtraColors
import systems.pkt.lingon.ui.theme.LocalLingonExtraColors
import systems.pkt.lingon.ui.TestTags

@Composable
fun TerminalView(
    lines: List<String>,
    textStyle: TextStyle,
    contentPadding: Dp,
    resetScrollKey: Any? = null,
    forceScrollToBottom: Boolean = false,
    modifier: Modifier = Modifier,
) {
    val listState = rememberLazyListState()
    var userScrolledUp by remember { mutableStateOf(false) }
    val lastIndex = lines.lastIndex
    val lastIndexState = rememberUpdatedState(lastIndex)

    val nestedScrollConnection = remember {
        object : NestedScrollConnection {
            override fun onPostScroll(
                consumed: Offset,
                available: Offset,
                source: NestedScrollSource,
            ): Offset {
                if (source == NestedScrollSource.UserInput) {
                    val lastVisible = listState.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: -1
                    val atBottom = lastVisible >= lastIndexState.value - 1
                    userScrolledUp = !atBottom
                }
                return Offset.Zero
            }
        }
    }

    LaunchedEffect(resetScrollKey) {
        userScrolledUp = false
        if (lastIndex >= 0) {
            listState.scrollToItem(lastIndex)
        }
    }

    LaunchedEffect(lines.size, forceScrollToBottom) {
        if ((forceScrollToBottom || !userScrolledUp) && lastIndex >= 0) {
            snapshotFlow { listState.layoutInfo.totalItemsCount }
                .first { it >= lines.size }
            listState.scrollToItem(lastIndex)
            userScrolledUp = false
        }
    }

    SelectionContainer {
        LazyColumn(
            state = listState,
            modifier = modifier
                .fillMaxSize()
                .padding(contentPadding)
                .testTag(TestTags.TerminalList)
                .nestedScroll(nestedScrollConnection),
        ) {
            itemsIndexed(lines) { _, line ->
                TerminalLine(line, textStyle)
            }
        }
    }
}

@Composable
private fun TerminalLine(line: String, baseStyle: TextStyle) {
    val parsed = parseLine(line)
    val extras = LocalLingonExtraColors.current
    val baseColor = when (parsed.kind) {
        LineKind.Command -> extras.muted
        LineKind.Meta -> extras.muted
        LineKind.Error -> MaterialTheme.colorScheme.error
        LineKind.Stderr -> extras.stderr
        LineKind.Reasoning -> extras.reasoning
        LineKind.Agent -> MaterialTheme.colorScheme.onSurface
        LineKind.Worked -> extras.muted
        LineKind.Help -> MaterialTheme.colorScheme.onSurface
        LineKind.AboutCopyright -> extras.aboutCopyright
        LineKind.AboutLink -> extras.aboutLink
        LineKind.AboutVersion -> MaterialTheme.colorScheme.onSurface
        LineKind.Normal -> MaterialTheme.colorScheme.onSurface
    }

    if (parsed.kind == LineKind.Worked) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = parsed.text.ifEmpty { "\u00a0" },
                color = baseColor,
                style = baseStyle.copy(fontStyle = FontStyle.Italic),
            )
            Spacer(modifier = Modifier.width(8.dp))
            HorizontalDivider(color = extras.border, modifier = Modifier.weight(1f))
        }
        return
    }

    if (parsed.kind == LineKind.AboutLink) {
        val uriHandler = LocalUriHandler.current
        val linkText = parsed.text.ifEmpty { "\u00a0" }
        Text(
            text = linkText,
            color = baseColor,
            style = baseStyle.copy(fontStyle = FontStyle.Italic, textDecoration = TextDecoration.Underline),
            modifier = Modifier.clickable(enabled = linkText.isNotBlank()) {
                uriHandler.openUri(linkText)
            },
        )
        return
    }

    if (parsed.markdown) {
        val annotated = buildAnnotatedString(parsed.text, parsed.kind, extras)
        Text(text = annotated, style = baseStyle, color = baseColor)
        return
    }

    Text(
        text = parsed.text.ifEmpty { "\u00a0" },
        color = baseColor,
        style = when (parsed.kind) {
            LineKind.Meta -> baseStyle.copy(fontStyle = FontStyle.Italic)
            LineKind.Reasoning -> baseStyle.copy(fontStyle = FontStyle.Italic)
            LineKind.AboutVersion -> baseStyle.copy(fontStyle = FontStyle.Italic, fontWeight = FontWeight.Bold)
            else -> baseStyle
        },
    )
}

private fun buildAnnotatedString(
    text: String,
    kind: LineKind,
    extras: LingonExtraColors,
): AnnotatedString {
    val spans = parseMarkdown(text)
    val builder = AnnotatedString.Builder()
    for (span in spans) {
        val color = when {
            kind == LineKind.Help && span.code -> extras.helpArg
            span.code -> extras.code
            kind == LineKind.Reasoning && span.bold -> extras.reasoningBold
            kind == LineKind.Help && span.bold -> extras.aboutLink
            else -> null
        }
        val style = if (color != null) {
            SpanStyle(
                color = color,
                fontWeight = if (span.bold) FontWeight.Bold else FontWeight.Normal,
                fontStyle = if (span.italic) FontStyle.Italic else FontStyle.Normal,
            )
        } else {
            SpanStyle(
                fontWeight = if (span.bold) FontWeight.Bold else FontWeight.Normal,
                fontStyle = if (span.italic) FontStyle.Italic else FontStyle.Normal,
            )
        }
        builder.pushStyle(style)
        builder.append(span.text)
        builder.pop()
    }
    if (builder.length == 0) {
        builder.append("\u00a0")
    }
    return builder.toAnnotatedString()
}

private const val COMMAND_MARKER = '\u001a'
private const val AGENT_MARKER = '\u001c'
private const val REASONING_MARKER = '\u001d'
private const val WORKED_MARKER = '\u001e'
private const val STDERR_MARKER = '\u001f'
private const val HELP_MARKER = '\u0016'
private const val ABOUT_VERSION_MARKER = '\u0017'
private const val ABOUT_COPYRIGHT_MARKER = '\u0018'
private const val ABOUT_LINK_MARKER = '\u0019'

private fun parseLine(line: String): ParsedLine {
    var text = line
    if (text.startsWith(WORKED_MARKER)) {
        return ParsedLine(text = text.drop(1), kind = LineKind.Worked)
    }
    if (text.startsWith(HELP_MARKER)) {
        return ParsedLine(text = text.drop(1), kind = LineKind.Help, markdown = true)
    }
    if (text.startsWith(ABOUT_VERSION_MARKER)) {
        return ParsedLine(text = text.drop(1), kind = LineKind.AboutVersion)
    }
    if (text.startsWith(ABOUT_COPYRIGHT_MARKER)) {
        return ParsedLine(text = text.drop(1), kind = LineKind.AboutCopyright)
    }
    if (text.startsWith(ABOUT_LINK_MARKER)) {
        return ParsedLine(text = text.drop(1), kind = LineKind.AboutLink)
    }
    if (text.startsWith(AGENT_MARKER)) {
        return ParsedLine(text = text.drop(1), kind = LineKind.Agent, markdown = true)
    }
    if (text.startsWith(REASONING_MARKER)) {
        return ParsedLine(text = text.drop(1), kind = LineKind.Reasoning, markdown = true)
    }
    if (text.startsWith(COMMAND_MARKER)) {
        return ParsedLine(text = text.drop(1), kind = LineKind.Command)
    }
    var stderr = false
    if (text.startsWith(STDERR_MARKER)) {
        stderr = true
        text = text.drop(1)
    }
    if (text.startsWith("error:")) return ParsedLine(text, LineKind.Error)
    if (text.startsWith("command failed:") || text.startsWith("command error:")) {
        return ParsedLine(text, LineKind.Error)
    }
    if (text.startsWith("--- command finished")) return ParsedLine(text, LineKind.Meta)
    if (stderr) return ParsedLine(text, LineKind.Stderr)
    return ParsedLine(text, LineKind.Normal)
}

private data class ParsedLine(
    val text: String,
    val kind: LineKind,
    val markdown: Boolean = false,
)

private enum class LineKind {
    Normal,
    Command,
    Reasoning,
    Agent,
    Error,
    Stderr,
    Meta,
    Worked,
    Help,
    AboutVersion,
    AboutCopyright,
    AboutLink,
}

private data class MarkdownSpan(
    val text: String,
    val bold: Boolean,
    val italic: Boolean,
    val code: Boolean,
)

private fun parseMarkdown(text: String): List<MarkdownSpan> {
    val spans = mutableListOf<MarkdownSpan>()
    if (text.isEmpty()) return spans
    var buf = StringBuilder()
    var bold = false
    var italic = false
    var code = false

    fun flush() {
        if (buf.isEmpty()) return
        spans.add(MarkdownSpan(buf.toString(), bold, italic, code))
        buf = StringBuilder()
    }

    var i = 0
    while (i < text.length) {
        val ch = text[i]
        if (ch == '\\' && i + 1 < text.length) {
            buf.append(text[i + 1])
            i += 2
            continue
        }
        if (ch == '`') {
            if (code) {
                flush()
                code = false
                i += 1
                continue
            }
            if (hasClosing(text.substring(i + 1), "`")) {
                flush()
                code = true
                i += 1
                continue
            }
        }
        if (!code && ch == '*') {
            if (text.startsWith("**", i)) {
                if (bold) {
                    flush()
                    bold = false
                    i += 2
                    continue
                }
                if (hasClosing(text.substring(i + 2), "**")) {
                    flush()
                    bold = true
                    i += 2
                    continue
                }
                buf.append("**")
                i += 2
                continue
            }
            if (italic) {
                flush()
                italic = false
                i += 1
                continue
            }
            if (hasClosing(text.substring(i + 1), "*")) {
                flush()
                italic = true
                i += 1
                continue
            }
        }
        buf.append(ch)
        i += 1
    }
    flush()
    return spans
}

private fun hasClosing(remaining: String, marker: String): Boolean {
    if (marker.isEmpty() || remaining.isEmpty()) return false
    return remaining.contains(marker)
}
