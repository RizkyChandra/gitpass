package com.gitpass.autofill

import com.gitpass.Entry

/** What the user is filling: a browser page, or a native app. */
data class Target(
    val packageName: String = "",
    val webDomain: String = "",
) {
    /** A human label for a newly saved entry. */
    val label: String
        get() = when {
            webDomain.isNotEmpty() -> registrableDomain(webDomain)
            else -> appLabel(packageName)
        }
}

/**
 * Reduces a host to something worth comparing: drops "www." and keeps the last
 * two labels, so login.github.com and github.com match each other.
 *
 * ponytail: no Public Suffix List, so co.uk-style domains collapse to "co.uk"
 * and would over-match. Pull in publicsuffix if that bites.
 */
fun registrableDomain(host: String): String {
    val clean = host.substringAfter("://").substringBefore('/').substringBefore(':')
        .removePrefix("www.").lowercase()
    val labels = clean.split('.').filter { it.isNotEmpty() }
    return if (labels.size <= 2) clean else labels.takeLast(2).joinToString(".")
}

/**
 * Turns com.example.myapp into "example" — the part most likely to appear in an
 * entry's name or URL. Reversed-domain package names put the brand second.
 */
fun appLabel(packageName: String): String {
    val parts = packageName.split('.').filter { it.isNotEmpty() }
    return when {
        parts.size >= 2 -> parts[1]
        parts.isNotEmpty() -> parts[0]
        else -> ""
    }
}

/**
 * Picks the entries worth offering for a target, best first.
 *
 * Scoring rather than filtering, because an over-eager filter shows nothing and
 * the user silently falls back to copy-paste, while a loose one merely shows an
 * extra row. Anything scoring zero is dropped.
 */
fun matchEntries(entries: List<Entry>, target: Target): List<Entry> {
    val domain = if (target.webDomain.isNotEmpty()) registrableDomain(target.webDomain) else ""
    val app = if (target.packageName.isNotEmpty()) appLabel(target.packageName) else ""
    if (domain.isEmpty() && app.isEmpty()) return emptyList()

    return entries
        .filter { !it.deleted }
        .map { it to score(it, domain, app) }
        .filter { it.second > 0 }
        .sortedWith(compareByDescending<Pair<Entry, Int>> { it.second }.thenBy { it.first.name })
        .map { it.first }
}

private fun score(entry: Entry, domain: String, app: String): Int {
    val name = entry.name.lowercase()
    val url = entry.url.lowercase()
    val entryDomain = if (url.isNotEmpty()) registrableDomain(url) else ""

    var score = 0
    if (domain.isNotEmpty()) {
        // Exact domain agreement is the strongest signal available.
        if (entryDomain == domain) score += 100
        if (registrableDomain(name) == domain) score += 80
        if (name.contains(domain)) score += 40
        // The bare brand, so "github" matches an entry called "GitHub work".
        val brand = domain.substringBefore('.')
        if (brand.length >= 3 && (name.contains(brand) || url.contains(brand))) score += 20
    }
    if (app.isNotEmpty() && app.length >= 3) {
        if (name.contains(app)) score += 50
        if (url.contains(app)) score += 30
    }
    return score
}
