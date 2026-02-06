package systems.pkt.lingon.data

import kotlinx.serialization.json.Json

val LingonJson = Json {
    ignoreUnknownKeys = true
    isLenient = true
    explicitNulls = false
}
