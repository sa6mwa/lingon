package systems.pkt.lingon.share

import java.io.ByteArrayOutputStream
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.util.Locale

object ShareTokens {
    private const val prefixBare = "LGB"
    private const val prefixEmbedded = "LGE"
    private const val versionByte: Byte = 1
    private const val randomSize = 20
    private const val maxEndpointLen = 2048
    private const val maxBareBodyChars = 37
    private const val maxEmbeddedBodyChars = 3317
    private const val maxCandidateChars = 3 + maxEmbeddedBodyChars * 2
    private const val alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
    private val decodeTable = IntArray(128) { -1 }.apply {
        for (i in alphabet.indices) {
            this[alphabet[i].code] = i
        }
    }

    enum class Kind {
        Bare,
        Embedded,
    }

    data class Parsed(
        val kind: Kind,
        val version: Byte,
        val random: ByteArray,
        val endpoint: String? = null,
    )

    fun parse(raw: String): Parsed? {
        val trimmed = raw.trim()
        if (trimmed.length < 4) return null
        if (trimmed.length > maxCandidateChars) return null
        val prefix = trimmed.substring(0, 3).uppercase(Locale.US)
        val body = trimmed.substring(3)
        return when (prefix) {
            prefixBare -> parseBare(body)
            prefixEmbedded -> parseEmbedded(body)
            else -> null
        }
    }

    fun findInText(text: String): Parsed? {
        val regex = Regex("(LGB|LGE)[0-9A-Za-z\\-]+", RegexOption.IGNORE_CASE)
        for (match in regex.findAll(text)) {
            val parsed = parse(match.value)
            if (parsed != null) {
                return parsed
            }
        }
        return null
    }

    fun bareToken(parsed: Parsed): String? {
        if (parsed.random.size != randomSize) return null
        return encodeBare(parsed.random)
    }

    fun encodeBare(random: ByteArray): String? {
        if (random.size != randomSize) return null
        val payload = ByteArrayOutputStream(1 + randomSize + 2)
        payload.write(byteArrayOf(versionByte))
        payload.write(random)
        val crc = crc16(payload.toByteArray())
        payload.write(byteArrayOf((crc.toInt() shr 8).toByte(), crc.toByte()))
        return prefixBare + encodeCrockford(payload.toByteArray())
    }

    fun encodeEmbedded(random: ByteArray, endpoint: String): String? {
        val trimmed = endpoint.trim()
        val endpointBytes = trimmed.toByteArray(Charsets.UTF_8)
        if (random.size != randomSize || endpointBytes.isEmpty() || endpointBytes.size > maxEndpointLen) return null
        val payload = ByteArrayOutputStream(1 + randomSize + 2 + endpointBytes.size + 2)
        payload.write(byteArrayOf(versionByte))
        payload.write(random)
        val lenBuf = ByteBuffer.allocate(2).order(ByteOrder.BIG_ENDIAN).putShort(endpointBytes.size.toShort())
        payload.write(lenBuf.array())
        payload.write(endpointBytes)
        val crc = crc16(payload.toByteArray())
        payload.write(byteArrayOf((crc.toInt() shr 8).toByte(), crc.toByte()))
        return prefixEmbedded + encodeCrockford(payload.toByteArray())
    }

    private fun parseBare(body: String): Parsed? {
        val raw = decodeCrockford(body, maxBareBodyChars) ?: return null
        val expected = 1 + randomSize + 2
        if (raw.size != expected) return null
        val version = raw[0]
        if (version != versionByte) return null
        val payload = raw.copyOfRange(0, 1 + randomSize)
        val crc = ((raw[1 + randomSize].toInt() and 0xff) shl 8) or (raw[1 + randomSize + 1].toInt() and 0xff)
        if (crc16(payload) != crc.toShort()) return null
        val random = raw.copyOfRange(1, 1 + randomSize)
        return Parsed(kind = Kind.Bare, version = version, random = random)
    }

    private fun parseEmbedded(body: String): Parsed? {
        val raw = decodeCrockford(body, maxEmbeddedBodyChars) ?: return null
        val minLen = 1 + randomSize + 2 + 2
        if (raw.size < minLen) return null
        val version = raw[0]
        if (version != versionByte) return null
        val lenStart = 1 + randomSize
        val endpointLen = ((raw[lenStart].toInt() and 0xff) shl 8) or (raw[lenStart + 1].toInt() and 0xff)
        if (endpointLen <= 0 || endpointLen > maxEndpointLen) return null
        val expected = 1 + randomSize + 2 + endpointLen + 2
        if (raw.size != expected) return null
        val endpointStart = lenStart + 2
        val endpoint = raw.copyOfRange(endpointStart, endpointStart + endpointLen).toString(Charsets.UTF_8)
        val payload = raw.copyOfRange(0, expected - 2)
        val crc = ((raw[expected - 2].toInt() and 0xff) shl 8) or (raw[expected - 1].toInt() and 0xff)
        if (crc16(payload) != crc.toShort()) return null
        val random = raw.copyOfRange(1, 1 + randomSize)
        return Parsed(kind = Kind.Embedded, version = version, random = random, endpoint = endpoint)
    }

    private fun encodeCrockford(data: ByteArray): String {
        val out = StringBuilder()
        var buffer = 0
        var bitsLeft = 0
        for (b in data) {
            buffer = (buffer shl 8) or (b.toInt() and 0xff)
            bitsLeft += 8
            while (bitsLeft >= 5) {
                val idx = (buffer shr (bitsLeft - 5)) and 0x1f
                bitsLeft -= 5
                out.append(alphabet[idx])
            }
        }
        if (bitsLeft > 0) {
            val idx = (buffer shl (5 - bitsLeft)) and 0x1f
            out.append(alphabet[idx])
        }
        return out.toString()
    }

    private fun decodeCrockford(input: String, maxCleanedChars: Int): ByteArray? {
        var buffer = 0
        var bitsLeft = 0
        var cleanedChars = 0
        val out = ByteArrayOutputStream()
        for (ch in input) {
            val cleaned = cleanCrockfordChar(ch) ?: continue
            cleanedChars++
            if (cleanedChars > maxCleanedChars) return null
            if (cleaned.code >= decodeTable.size) return null
            val value = decodeTable[cleaned.code]
            if (value < 0) return null
            buffer = (buffer shl 5) or value
            bitsLeft += 5
            while (bitsLeft >= 8) {
                val byteVal = (buffer shr (bitsLeft - 8)) and 0xff
                bitsLeft -= 8
                out.write(byteVal)
            }
        }
        if (cleanedChars == 0) return null
        return out.toByteArray()
    }

    private fun cleanCrockfordChar(ch: Char): Char? {
        return when (ch) {
            '-', ' ', '\n', '\r', '\t' -> null
            'o', 'O' -> '0'
            'i', 'I', 'l', 'L' -> '1'
            else -> ch.uppercaseChar()
        }
    }

    private fun crc16(data: ByteArray): Short {
        var crc = 0xffff
        for (b in data) {
            crc = crc xor ((b.toInt() and 0xff) shl 8)
            repeat(8) {
                crc = if (crc and 0x8000 != 0) {
                    (crc shl 1) xor 0x1021
                } else {
                    crc shl 1
                }
                crc = crc and 0xffff
            }
        }
        return crc.toShort()
    }
}
