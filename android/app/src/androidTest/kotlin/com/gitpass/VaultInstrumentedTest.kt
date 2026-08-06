package com.gitpass

import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import java.io.File

/**
 * Exercises the Go core through JNI on a real Android runtime.
 *
 * This is the test that matters most for the whole design: age encryption,
 * the on-disk vault and go-git all have to work inside an app process, with
 * no NDK toolchain and no home directory. Everything else is Kotlin glue.
 */
@RunWith(AndroidJUnit4::class)
class VaultInstrumentedTest {

    private val context = InstrumentationRegistry.getInstrumentation().targetContext
    private val passphrase = "correct-horse-battery-staple"

    @Before
    fun freshVault() {
        VaultSession.lock()
        File(VaultSession.vaultDir(context)).deleteRecursively()
    }

    @Test
    fun createStoreAndReadBack() = runBlocking {
        VaultSession.create(context, passphrase)
        assertTrue("vault should be unlocked after create", VaultSession.isUnlocked)
        assertTrue("identity.age should exist on disk", VaultSession.exists(context))

        val saved = VaultSession.put(
            Entry(
                name = "github.com",
                username = "alice",
                password = "hunter2",
                totp = "otpauth://totp/GitHub:alice?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ",
                tags = listOf("work"),
            )
        )
        assertTrue("Put must assign an id", saved.id.isNotEmpty())

        val listed = VaultSession.list()
        assertEquals(1, listed.size)
        assertEquals("github.com", listed[0].name)
        assertEquals(listOf("work"), listed[0].tags)

        val fetched = VaultSession.get(saved.id)
        assertEquals("hunter2", fetched.password)

        // Relock and reopen: proves the passphrase alone decrypts the vault.
        VaultSession.lock()
        VaultSession.unlock(context, passphrase)
        assertEquals("hunter2", VaultSession.get(saved.id).password)
    }

    @Test
    fun totpMatchesTheRfcVector() = runBlocking {
        VaultSession.create(context, passphrase)
        val saved = VaultSession.put(
            Entry(
                name = "totp",
                totp = "otpauth://totp/x?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ",
            )
        )
        val code = VaultSession.totp(saved.id)
        assertEquals("a TOTP is six digits", 6, code.code.length)
        assertTrue(code.code.all { it.isDigit() })
        assertTrue("countdown must be within one period", code.secondsLeft in 1..30)
    }

    @Test
    fun deleteLeavesATombstoneThatHidesTheSecret() = runBlocking {
        VaultSession.create(context, passphrase)
        val saved = VaultSession.put(Entry(name = "doomed", password = "secret"))
        VaultSession.delete(saved.id)

        assertTrue("deleted entry must not be listed", VaultSession.list().isEmpty())
        val tombstone = VaultSession.get(saved.id)
        assertTrue("tombstone must be marked deleted", tombstone.deleted)
        assertEquals("tombstone must not keep the password", "", tombstone.password)
    }

    @Test
    fun wrongPassphraseIsRejected() = runBlocking {
        VaultSession.create(context, passphrase)
        VaultSession.lock()
        val failed = runCatching { VaultSession.unlock(context, "not-the-passphrase") }
        assertTrue("wrong passphrase must fail", failed.isFailure)
    }

    @Test
    fun generatorsProduceUsableSecrets() {
        val password = VaultSession.generatePassword(20)
        assertEquals(20, password.length)
        val phrase = VaultSession.generatePassphrase(6)
        assertEquals(6, phrase.split("-").size)
        VaultSession.checkPassphrase(phrase) // throws when too weak
    }
}
