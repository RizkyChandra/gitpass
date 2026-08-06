package com.gitpass.autofill

import com.gitpass.Entry
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class MatchingTest {

    private val entries = listOf(
        Entry(id = "1", name = "github.com", username = "alice", url = "https://github.com/login"),
        Entry(id = "2", name = "GitHub work", username = "alice@work", url = "https://github.com"),
        Entry(id = "3", name = "bank.example", username = "bob", url = "https://bank.example"),
        Entry(id = "4", name = "old github", username = "gone", deleted = true),
    )

    @Test
    fun `registrable domain drops subdomains and www`() {
        assertEquals("github.com", registrableDomain("https://login.github.com/session"))
        assertEquals("github.com", registrableDomain("www.github.com"))
        assertEquals("github.com", registrableDomain("github.com:443"))
    }

    @Test
    fun `app label takes the brand out of a reversed package name`() {
        assertEquals("github", appLabel("com.github.android"))
        assertEquals("example", appLabel("com.example"))
    }

    @Test
    fun `web domain matches both github entries and not the bank`() {
        val hits = matchEntries(entries, Target(webDomain = "https://github.com/login"))
        assertEquals(listOf("1", "2"), hits.map { it.id })
    }

    @Test
    fun `native package matches by brand`() {
        val hits = matchEntries(entries, Target(packageName = "com.github.android"))
        assertTrue(hits.isNotEmpty())
        assertTrue(hits.all { it.name.contains("github", true) })
    }

    @Test
    fun `tombstones are never offered`() {
        val hits = matchEntries(entries, Target(webDomain = "github.com"))
        assertTrue(hits.none { it.deleted })
    }

    @Test
    fun `an unrelated site matches nothing`() {
        assertEquals(emptyList<Entry>(), matchEntries(entries, Target(webDomain = "totally-unrelated.test")))
    }

    @Test
    fun `no target means no suggestions rather than everything`() {
        assertEquals(emptyList<Entry>(), matchEntries(entries, Target()))
    }
}
