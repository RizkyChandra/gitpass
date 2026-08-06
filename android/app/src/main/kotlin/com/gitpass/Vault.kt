package com.gitpass

import android.content.Context
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import java.io.File

/**
 * Mirrors the Go `vault.Entry`. Every field defaults, because Go marshals with
 * `omitempty` and simply leaves empty ones out of the JSON.
 */
@Serializable
data class Entry(
    val id: String = "",
    val name: String = "",
    val username: String = "",
    val email: String = "",
    val password: String = "",
    val totp: String = "",
    val url: String = "",
    val notes: String = "",
    val tags: List<String> = emptyList(),
    @SerialName("updated_at") val updatedAt: String = "",
    val deleted: Boolean = false,
) {
    /** What to show as the account line: username, falling back to email. */
    val account: String get() = username.ifEmpty { email }
}

@Serializable
data class TotpCode(
    val code: String = "",
    @SerialName("seconds_left") val secondsLeft: Int = 0,
)

/**
 * Holds the unlocked vault for the whole process.
 *
 * It has to be process-wide rather than tied to an Activity because the
 * autofill service runs without any Activity of ours in the foreground, and
 * must be able to answer a fill request from an already-unlocked vault.
 *
 * ponytail: no unlock timeout — the vault stays open until the process dies.
 * Add an idle lock if that turns out to matter.
 */
object VaultSession {

    private val json = Json {
        ignoreUnknownKeys = true
        encodeDefaults = true
    }

    @Volatile
    private var vault: gitpass.Vault? = null

    val isUnlocked: Boolean get() = vault != null

    private fun require(): gitpass.Vault =
        vault ?: throw IllegalStateException("vault is locked")

    fun vaultDir(context: Context): String =
        File(context.filesDir, "vault").absolutePath

    fun exists(context: Context): Boolean =
        File(vaultDir(context), "identity.age").exists()

    /** Point the Go side at app-private storage: Android has no home directory. */
    private fun configure(context: Context) {
        gitpass.Gitpass.setCredsDir(context.filesDir.absolutePath)
    }

    suspend fun create(context: Context, passphrase: String) = withContext(Dispatchers.IO) {
        configure(context)
        vault = gitpass.Gitpass.`init`(vaultDir(context), passphrase)
    }

    suspend fun unlock(context: Context, passphrase: String) = withContext(Dispatchers.IO) {
        configure(context)
        vault = gitpass.Gitpass.open(vaultDir(context), passphrase)
    }

    /** Clone a remote vault, then unlock it with the same passphrase. */
    suspend fun cloneAndUnlock(
        context: Context,
        url: String,
        token: String,
        passphrase: String,
    ) = withContext(Dispatchers.IO) {
        configure(context)
        gitpass.Gitpass.clone(vaultDir(context), url, token)
        vault = gitpass.Gitpass.open(vaultDir(context), passphrase)
        if (token.isNotEmpty()) require().setToken(token)
    }

    fun lock() {
        vault = null
    }

    suspend fun list(): List<Entry> = withContext(Dispatchers.IO) {
        json.decodeFromString(require().list())
    }

    suspend fun get(id: String): Entry = withContext(Dispatchers.IO) {
        json.decodeFromString(require().get(id))
    }

    suspend fun put(entry: Entry): Entry = withContext(Dispatchers.IO) {
        json.decodeFromString(require().put(json.encodeToString(entry)))
    }

    suspend fun delete(id: String) = withContext(Dispatchers.IO) {
        require().delete(id)
    }

    suspend fun totp(id: String): TotpCode = withContext(Dispatchers.IO) {
        json.decodeFromString(require().totp(id))
    }

    suspend fun sync(): String = withContext(Dispatchers.IO) {
        require().sync()
    }

    suspend fun setRemote(url: String) = withContext(Dispatchers.IO) {
        require().setRemote(url)
    }

    suspend fun setToken(token: String) = withContext(Dispatchers.IO) {
        require().setToken(token)
    }

    suspend fun gc(days: Int): Long = withContext(Dispatchers.IO) {
        require().gc(days.toLong())
    }

    /** Blocking read for the autofill service, which is already off the main thread. */
    fun listBlocking(): List<Entry> = json.decodeFromString(require().list())

    fun putBlocking(entry: Entry): Entry =
        json.decodeFromString(require().put(json.encodeToString(entry)))

    fun generatePassword(length: Int): String =
        gitpass.Gitpass.generatePassword(length.toLong())

    fun generatePassphrase(words: Int): String =
        gitpass.Gitpass.generatePassphrase(words.toLong())

    /** Throws with the reason when the passphrase is too weak. */
    fun checkPassphrase(passphrase: String) =
        gitpass.Gitpass.checkPassphrase(passphrase)
}
