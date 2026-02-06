package systems.pkt.lingon.data

object AppLockTimeoutPolicy {
    val optionsMinutes: List<Int> = listOf(0, 3, 5, 15, 30, 45, 60)
    const val defaultTimeoutMinutes: Int = 30

    fun normalize(minutes: Int): Int {
        if (minutes <= 0) return 0

        var selected = 0
        for (option in optionsMinutes) {
            if (option <= minutes) {
                selected = option
                continue
            }
            break
        }
        return selected
    }
}
