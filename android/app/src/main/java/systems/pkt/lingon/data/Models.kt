package systems.pkt.lingon.data

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class AuthTokens(
    @SerialName("access_token")
    val accessToken: String = "",
    @SerialName("refresh_token")
    val refreshToken: String = "",
    @SerialName("access_expires_at")
    val accessExpiresAt: String = "",
    @SerialName("refresh_expires_at")
    val refreshExpiresAt: String = "",
)

@Serializable
data class ErrorResponse(
    @SerialName("error")
    val error: String = "request failed",
)
