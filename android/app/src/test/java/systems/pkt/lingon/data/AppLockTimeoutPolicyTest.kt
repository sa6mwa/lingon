package systems.pkt.lingon.data

import org.junit.Assert.assertEquals
import org.junit.Test

class AppLockTimeoutPolicyTest {
    @Test
    fun `options are fixed and ordered`() {
        assertEquals(listOf(0, 3, 5, 15, 30, 45, 60), AppLockTimeoutPolicy.optionsMinutes)
    }

    @Test
    fun `normalize clamps to nearest lower supported option`() {
        val cases = mapOf(
            -10 to 0,
            0 to 0,
            1 to 0,
            2 to 0,
            3 to 3,
            4 to 3,
            5 to 5,
            14 to 5,
            15 to 15,
            44 to 30,
            45 to 45,
            59 to 45,
            60 to 60,
            61 to 60,
            10_000 to 60,
        )

        cases.forEach { (input, expected) ->
            assertEquals("normalize($input)", expected, AppLockTimeoutPolicy.normalize(input))
        }
    }
}
