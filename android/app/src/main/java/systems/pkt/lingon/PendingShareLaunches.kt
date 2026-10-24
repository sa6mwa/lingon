package systems.pkt.lingon

import java.util.UUID

internal data class PendingShareLaunch(
    val shareToken: String?,
    val endpointOverride: String?,
)

internal object PendingShareLaunches {
    private val lock = Any()
    private val pending = LinkedHashMap<String, PendingShareLaunch>()

    fun put(shareToken: String?, endpointOverride: String?): String {
        val id = UUID.randomUUID().toString()
        synchronized(lock) {
            pending[id] = PendingShareLaunch(shareToken, endpointOverride)
        }
        return id
    }

    fun take(id: String?): PendingShareLaunch? {
        if (id.isNullOrBlank()) return null
        synchronized(lock) {
            return pending.remove(id)
        }
    }
}
