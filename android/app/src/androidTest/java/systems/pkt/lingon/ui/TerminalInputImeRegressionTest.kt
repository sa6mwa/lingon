package systems.pkt.lingon.ui

import android.view.View
import android.view.ViewGroup
import android.view.inputmethod.EditorInfo
import android.view.inputmethod.InputConnection
import android.text.Editable
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import systems.pkt.lingon.MainActivity
import kotlin.Function1

@RunWith(AndroidJUnit4::class)
class TerminalInputImeRegressionTest {
    @get:Rule
    val composeRule = createAndroidComposeRule<MainActivity>()

    @Test
    fun composingDeleteVariants_doNotEmit_terminal_backspace() {
        composeRule.activityRule.scenario.onActivity { activity ->
            val variants = listOf(
                "deleteSurroundingText" to { connection: InputConnection ->
                    connection.deleteSurroundingText(1, 0)
                },
                "deleteSurroundingTextInCodePoints" to { connection: InputConnection ->
                    connection.deleteSurroundingTextInCodePoints(1, 0)
                },
            )

            variants.forEach { (label, delete) ->
                val recorder = Recorder()
                val view = newTerminalInputView(activity)
                activity.findViewById<ViewGroup>(android.R.id.content).addView(view)
                view.setFocusHooks(recorder)
                view.requestFocus()

                val connection = view.onCreateInputConnection(EditorInfo())
                    ?: throw AssertionError("missing input connection for $label")

                assertTrue("$label should accept composing text", connection.setComposingText("ab", 1))
                assertTrue("$label should accept delete", delete(connection))
                assertTrue("$label should finish composing", connection.finishComposingText())

                assertEquals("$label should keep composing delete out of terminal backspace", 0, recorder.backspaces.size)
                assertEquals("$label should commit the remaining composing text", listOf("a"), recorder.commits)
                assertEquals("$label should leave the remote shell with the final replacement only", "a", recorder.shell.text.toString())
            }
        }
    }

    @Test
    fun emptyBufferDeleteVariants_forward_terminal_backspace() {
        composeRule.activityRule.scenario.onActivity { activity ->
            val variants = listOf(
                "deleteSurroundingText" to { connection: InputConnection ->
                    connection.deleteSurroundingText(1, 0)
                },
                "deleteSurroundingTextInCodePoints" to { connection: InputConnection ->
                    connection.deleteSurroundingTextInCodePoints(1, 0)
                },
            )

            variants.forEach { (label, delete) ->
                val recorder = Recorder()
                val view = newTerminalInputView(activity)
                activity.findViewById<ViewGroup>(android.R.id.content).addView(view)
                view.setFocusHooks(recorder)
                view.requestFocus()

                val connection = view.onCreateInputConnection(EditorInfo())
                    ?: throw AssertionError("missing input connection for $label")

                delete(connection)
                assertEquals("$label should forward empty-buffer delete as terminal backspace", listOf(1), recorder.backspaces)
                assertEquals("$label should not commit text on plain backspace", emptyList<String>(), recorder.commits)
                assertEquals("$label should leave the remote shell unchanged for empty-buffer delete", "", recorder.shell.text.toString())
            }
        }
    }

    @Test
    fun committedTextDeleteVariants_keep_mirrored_buffer_and_forward_backspace() {
        composeRule.activityRule.scenario.onActivity { activity ->
            val variants = listOf(
                "deleteSurroundingText" to { connection: InputConnection ->
                    connection.deleteSurroundingText(1, 0)
                },
                "deleteSurroundingTextInCodePoints" to { connection: InputConnection ->
                    connection.deleteSurroundingTextInCodePoints(1, 0)
                },
            )

            variants.forEach { (label, delete) ->
                val recorder = Recorder()
                val view = newTerminalInputView(activity)
                activity.findViewById<ViewGroup>(android.R.id.content).addView(view)
                view.setFocusHooks(recorder)
                view.requestFocus()

                val connection = view.onCreateInputConnection(EditorInfo())
                    ?: throw AssertionError("missing input connection for $label")

                assertTrue("$label should accept committed text", connection.commitText("ab", 1))
                assertEquals("$label should mirror committed text in the editable buffer", "ab", view.readInputBuffer())
                assertEquals("$label should send the committed text to the remote shell", "ab", recorder.shell.text.toString())

                delete(connection)

                assertEquals("$label should remove one mirrored character after delete", "a", view.readInputBuffer())
                assertEquals("$label should forward delete as terminal backspace", listOf(1), recorder.backspaces)
                assertEquals("$label should emit the committed text once", listOf("ab"), recorder.commits)
                assertEquals("$label should leave the remote shell with the replaced line", "a", recorder.shell.text.toString())
            }
        }
    }

    @Test
    fun committedTextReplacementVariant_replaces_remote_shell_text() {
        composeRule.activityRule.scenario.onActivity { activity ->
            val recorder = Recorder()
            val view = newTerminalInputView(activity)
            activity.findViewById<ViewGroup>(android.R.id.content).addView(view)
            view.setFocusHooks(recorder)
            view.requestFocus()

            val connection = view.onCreateInputConnection(EditorInfo())
                ?: throw AssertionError("missing input connection")

            assertTrue("initial commit should succeed", connection.commitText("foo", 1))
            assertEquals("initial commit should reach the remote shell", "foo", recorder.shell.text.toString())

            assertTrue("selection should be accepted", connection.setSelection(0, 3))
            assertTrue("replacement commit should succeed", connection.commitText("bar", 1))

            assertEquals(
                "replacement should delete the old text and emit the new text",
                listOf("foo", "bar"),
                recorder.commits,
            )
            assertEquals(
                "replacement should keep the remote shell text in sync",
                "bar",
                recorder.shell.text.toString(),
            )
            assertEquals(
                "replacement should delete the replaced text before inserting the new text",
                listOf(3),
                recorder.backspaces,
            )
        }
    }

    private data class Recorder(
        val commits: MutableList<String> = mutableListOf(),
        val backspaces: MutableList<Int> = mutableListOf(),
        val shell: RemoteShell = RemoteShell(),
    )

    private data class RemoteShell(
        val text: StringBuilder = StringBuilder(),
    ) {
        fun insert(payload: String) {
            text.append(payload)
        }

        fun backspace(count: Int) {
            repeat(count.coerceAtLeast(0)) {
                if (text.isNotEmpty()) {
                    text.deleteCharAt(text.length - 1)
                }
            }
        }
    }

    private fun newTerminalInputView(activity: MainActivity): View {
        val clazz = Class.forName("systems.pkt.lingon.ui.TerminalInputView")
        val ctor = clazz.getDeclaredConstructor(android.content.Context::class.java)
        ctor.isAccessible = true
        return ctor.newInstance(activity) as View
    }

    private fun View.setFocusHooks(recorder: Recorder) {
        invokeSetter(
            name = "setOnCommitText",
            listener = { text: String ->
                recorder.commits.add(text)
                recorder.shell.insert(text)
            },
        )
        invokeSetter(
            name = "setOnBackspace",
            listener = { count: Int ->
                recorder.backspaces.add(count)
                recorder.shell.backspace(count)
            },
        )
    }

    private fun View.readInputBuffer(): String {
        val field = javaClass.getDeclaredField("inputBuffer")
        field.isAccessible = true
        return (field.get(this) as Editable).toString()
    }

    private fun View.invokeSetter(name: String, listener: Any) {
        val method = javaClass.getDeclaredMethod(name, Function1::class.java)
        method.isAccessible = true
        method.invoke(this, listener)
    }
}
