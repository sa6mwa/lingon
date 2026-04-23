@file:OptIn(kotlinx.serialization.ExperimentalSerializationApi::class)

package systems.pkt.lingon.data.relay

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonNames

@Serializable
data class RelaySession(
    @SerialName("id")
    val id: String = "",
    @JsonNames("name", "Name")
    val name: String? = null,
    @SerialName("status")
    val status: String? = null,
    @SerialName("created_at")
    val createdAt: String? = null,
    @SerialName("last_active_at")
    val lastActiveAt: String? = null,
    @SerialName("username")
    val username: String? = null,
    @SerialName("headless")
    val headless: Boolean = false,
)

@Serializable
data class RelayWallEvent(
    @SerialName("id")
    val id: Long = 0,
    @SerialName("session_id")
    val sessionId: String? = null,
    @SerialName("kind")
    val kind: Int = 0,
    @SerialName("sender")
    val sender: String = "",
    @SerialName("session_name")
    val sessionName: String? = null,
    @SerialName("message")
    val message: String = "",
    @SerialName("timeout_seconds")
    val timeoutSeconds: Int = 5,
    @SerialName("created_at")
    val createdAt: String? = null,
)

@Serializable
data class RelayWallEventsPage(
    @SerialName("events")
    val events: List<RelayWallEvent> = emptyList(),
    @SerialName("next_id")
    val nextId: Long = 0,
    @SerialName("has_more")
    val hasMore: Boolean = false,
)
