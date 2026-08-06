package com.gitpass

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.ArrowBack
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FloatingActionButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

// ---------------------------------------------------------------- setup

/** First run: create a vault, or clone one that already exists. */
@Composable
fun SetupScreen(onReady: () -> Unit) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var cloning by remember { mutableStateOf(false) }
    var passphrase by remember { mutableStateOf("") }
    var url by remember { mutableStateOf("") }
    var token by remember { mutableStateOf("") }
    var error by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var suggestion by remember { mutableStateOf("") }

    LaunchedEffect(Unit) {
        suggestion = runCatching { VaultSession.generatePassphrase(6) }.getOrDefault("")
    }

    Column(
        Modifier
            .fillMaxSize()
            .padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text("gitpass", style = MaterialTheme.typography.headlineMedium)
        Text(
            if (cloning) "Clone the vault you already have." else "Create a new vault on this device.",
            style = MaterialTheme.typography.bodyMedium,
        )

        if (cloning) {
            OutlinedTextField(
                value = url,
                onValueChange = { url = it },
                label = { Text("Repository URL") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            OutlinedTextField(
                value = token,
                onValueChange = { token = it },
                label = { Text("Access token (blank for ssh or public)") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
        } else if (suggestion.isNotEmpty()) {
            // The key file lives inside the repo, so this passphrase is the
            // only thing protecting it once pushed.
            Text(
                "Suggested passphrase (about 77 bits):",
                style = MaterialTheme.typography.labelMedium,
            )
            Text(suggestion, style = MaterialTheme.typography.bodyLarge)
        }

        OutlinedTextField(
            value = passphrase,
            onValueChange = { passphrase = it; error = "" },
            label = { Text("Passphrase") },
            singleLine = true,
            visualTransformation = PasswordVisualTransformation(),
            modifier = Modifier.fillMaxWidth(),
        )

        if (error.isNotEmpty()) {
            Text(error, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
        }

        Button(
            onClick = {
                busy = true
                error = ""
                scope.launch {
                    try {
                        if (cloning) {
                            VaultSession.cloneAndUnlock(context, url.trim(), token.trim(), passphrase)
                        } else {
                            VaultSession.checkPassphrase(passphrase)
                            VaultSession.create(context, passphrase)
                        }
                        onReady()
                    } catch (e: Exception) {
                        error = e.message ?: "Failed"
                        busy = false
                    }
                }
            },
            enabled = passphrase.isNotEmpty() && !busy && (!cloning || url.isNotBlank()),
            modifier = Modifier.fillMaxWidth(),
        ) {
            Text(if (cloning) "Clone and unlock" else "Create vault")
        }

        TextButton(onClick = { cloning = !cloning; error = "" }) {
            Text(if (cloning) "Create a new vault instead" else "I already have a vault")
        }
    }
}

// --------------------------------------------------------------- unlock

@Composable
fun UnlockScreen(onUnlocked: () -> Unit) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var passphrase by remember { mutableStateOf("") }
    var error by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }

    Column(
        Modifier
            .fillMaxSize()
            .padding(24.dp),
        verticalArrangement = Arrangement.Center,
    ) {
        Text("gitpass", style = MaterialTheme.typography.headlineMedium)
        Text("Locked", style = MaterialTheme.typography.bodyMedium)
        OutlinedTextField(
            value = passphrase,
            onValueChange = { passphrase = it; error = "" },
            label = { Text("Passphrase") },
            singleLine = true,
            visualTransformation = PasswordVisualTransformation(),
            modifier = Modifier
                .fillMaxWidth()
                .padding(top = 16.dp),
        )
        if (error.isNotEmpty()) {
            Text(error, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
        }
        Button(
            onClick = {
                busy = true
                scope.launch {
                    try {
                        VaultSession.unlock(context, passphrase)
                        onUnlocked()
                    } catch (e: Exception) {
                        error = e.message ?: "Wrong passphrase"
                        busy = false
                    }
                }
            },
            enabled = passphrase.isNotEmpty() && !busy,
            modifier = Modifier
                .fillMaxWidth()
                .padding(top = 16.dp),
        ) {
            Text(if (busy) "Unlocking…" else "Unlock")
        }
    }
}

// ----------------------------------------------------------------- list

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ListScreen(
    onOpen: (Entry) -> Unit,
    onAdd: () -> Unit,
    onSettings: () -> Unit,
    onLock: () -> Unit,
) {
    val scope = rememberCoroutineScope()
    val snackbar = remember { SnackbarHostState() }
    var entries by remember { mutableStateOf<List<Entry>>(emptyList()) }
    var query by remember { mutableStateOf("") }
    var loading by remember { mutableStateOf(true) }
    var syncing by remember { mutableStateOf(false) }

    suspend fun reload() {
        entries = runCatching { VaultSession.list() }.getOrDefault(emptyList())
        loading = false
    }

    LaunchedEffect(Unit) { reload() }

    val shown = entries.filter {
        query.isBlank() ||
            it.name.contains(query, true) ||
            it.account.contains(query, true) ||
            it.tags.any { tag -> tag.contains(query, true) }
    }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = { Text("gitpass") },
                actions = {
                    IconButton(
                        enabled = !syncing,
                        onClick = {
                            syncing = true
                            scope.launch {
                                val result = runCatching { VaultSession.sync() }
                                syncing = false
                                snackbar.showSnackbar(
                                    result.getOrElse { "Sync failed: ${it.message}" },
                                )
                                reload()
                            }
                        },
                    ) { Icon(Icons.Default.Refresh, contentDescription = "Sync") }
                    IconButton(onClick = onSettings) {
                        Icon(Icons.Default.Settings, contentDescription = "Settings")
                    }
                    IconButton(onClick = onLock) {
                        Icon(Icons.Default.Lock, contentDescription = "Lock")
                    }
                },
            )
        },
        floatingActionButton = {
            FloatingActionButton(onClick = onAdd) {
                Icon(Icons.Default.Add, contentDescription = "Add")
            }
        },
    ) { padding ->
        Column(Modifier.padding(padding)) {
            if (syncing) LinearProgressIndicator(Modifier.fillMaxWidth())
            OutlinedTextField(
                value = query,
                onValueChange = { query = it },
                label = { Text("Search") },
                singleLine = true,
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 16.dp, vertical = 8.dp),
            )
            when {
                loading -> Box(Modifier.fillMaxSize(), Alignment.Center) { CircularProgressIndicator() }
                shown.isEmpty() -> Box(Modifier.fillMaxSize(), Alignment.Center) {
                    Text(if (entries.isEmpty()) "No entries yet" else "No matches")
                }

                else -> LazyColumn {
                    items(shown, key = { it.id }) { entry ->
                        Column(
                            Modifier
                                .fillMaxWidth()
                                .clickable { onOpen(entry) }
                                .padding(horizontal = 16.dp, vertical = 12.dp),
                        ) {
                            Text(entry.name, style = MaterialTheme.typography.bodyLarge)
                            val subtitle = buildString {
                                append(entry.account)
                                if (entry.tags.isNotEmpty()) {
                                    if (isNotEmpty()) append("  ")
                                    append(entry.tags.joinToString(" ") { "#$it" })
                                }
                            }
                            if (subtitle.isNotBlank()) {
                                Text(subtitle, style = MaterialTheme.typography.bodySmall)
                            }
                        }
                        HorizontalDivider()
                    }
                }
            }
        }
    }
}

// --------------------------------------------------------------- detail

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DetailScreen(
    id: String,
    onBack: () -> Unit,
    onEdit: (Entry) -> Unit,
    onDeleted: () -> Unit,
) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val snackbar = remember { SnackbarHostState() }
    var entry by remember { mutableStateOf(Entry()) }
    var reveal by remember { mutableStateOf(false) }
    var confirmDelete by remember { mutableStateOf(false) }
    var code by remember { mutableStateOf(TotpCode()) }

    LaunchedEffect(id) {
        entry = runCatching { VaultSession.get(id) }.getOrDefault(Entry())
    }

    // Refresh the TOTP once a second while this screen is on top.
    LaunchedEffect(entry.id, entry.totp) {
        if (entry.totp.isEmpty()) return@LaunchedEffect
        while (true) {
            code = runCatching { VaultSession.totp(entry.id) }.getOrDefault(TotpCode())
            delay(1000)
        }
    }

    fun copy(label: String, value: String) {
        if (value.isEmpty()) return
        copyToClipboard(context, label, value)
        scope.launch { snackbar.showSnackbar("$label copied") }
    }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = { Text(entry.name) },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.Default.ArrowBack, contentDescription = "Back")
                    }
                },
                actions = {
                    IconButton(onClick = { onEdit(entry) }) {
                        Icon(Icons.Default.Edit, contentDescription = "Edit")
                    }
                    IconButton(onClick = { confirmDelete = true }) {
                        Icon(Icons.Default.Delete, contentDescription = "Delete")
                    }
                },
            )
        },
    ) { padding ->
        Column(
            Modifier
                .padding(padding)
                .padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            if (entry.account.isNotEmpty()) {
                Field("account", entry.account) { copy("Username", entry.account) }
            }
            if (entry.password.isNotEmpty()) {
                Field(
                    label = "password",
                    value = if (reveal) entry.password else "••••••••••••",
                    onClick = { copy("Password", entry.password) },
                    trailing = {
                        TextButton(onClick = { reveal = !reveal }) {
                            Text(if (reveal) "Hide" else "Show")
                        }
                    },
                )
            }
            if (entry.totp.isNotEmpty()) {
                Field(
                    label = "code",
                    value = code.code.ifEmpty { "…" },
                    onClick = { copy("Code", code.code) },
                    trailing = { Text("${code.secondsLeft}s") },
                )
            }
            if (entry.url.isNotEmpty()) Field("url", entry.url) { copy("URL", entry.url) }
            if (entry.tags.isNotEmpty()) Field("tags", entry.tags.joinToString(" ")) {}
            if (entry.notes.isNotEmpty()) {
                Text("notes", style = MaterialTheme.typography.labelMedium)
                Text(entry.notes, style = MaterialTheme.typography.bodyMedium)
            }
        }
    }

    if (confirmDelete) {
        AlertDialog(
            onDismissRequest = { confirmDelete = false },
            title = { Text("Delete ${entry.name}?") },
            text = { Text("It stays in git history and can be recovered there.") },
            confirmButton = {
                TextButton(onClick = {
                    scope.launch {
                        runCatching { VaultSession.delete(entry.id) }
                        confirmDelete = false
                        onDeleted()
                    }
                }) { Text("Delete") }
            },
            dismissButton = {
                TextButton(onClick = { confirmDelete = false }) { Text("Cancel") }
            },
        )
    }
}

@Composable
private fun Field(
    label: String,
    value: String,
    trailing: @Composable (() -> Unit)? = null,
    onClick: () -> Unit,
) {
    Row(
        Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(label, style = MaterialTheme.typography.labelMedium)
            Text(value, style = MaterialTheme.typography.bodyLarge)
        }
        trailing?.invoke()
    }
}

// ----------------------------------------------------------------- edit

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun EditScreen(initial: Entry, onCancel: () -> Unit, onSaved: (Entry) -> Unit) {
    val scope = rememberCoroutineScope()
    var name by remember { mutableStateOf(initial.name) }
    var username by remember { mutableStateOf(initial.username) }
    var email by remember { mutableStateOf(initial.email) }
    var password by remember { mutableStateOf(initial.password) }
    var totp by remember { mutableStateOf(initial.totp) }
    var url by remember { mutableStateOf(initial.url) }
    var tags by remember { mutableStateOf(initial.tags.joinToString(" ")) }
    var notes by remember { mutableStateOf(initial.notes) }
    var error by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(if (initial.id.isEmpty()) "New entry" else "Edit") },
                navigationIcon = {
                    IconButton(onClick = onCancel) {
                        Icon(Icons.Default.ArrowBack, contentDescription = "Cancel")
                    }
                },
            )
        },
    ) { padding ->
        Column(
            Modifier
                .padding(padding)
                .padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Line("Name", name) { name = it }
            Line("Username", username) { username = it }
            Line("Email", email, KeyboardType.Email) { email = it }
            Row(verticalAlignment = Alignment.CenterVertically) {
                Box(Modifier.weight(1f)) { Line("Password", password) { password = it } }
                TextButton(onClick = {
                    password = runCatching { VaultSession.generatePassword(20) }.getOrDefault(password)
                }) { Text("Generate") }
            }
            Line("TOTP (otpauth:// or secret)", totp) { totp = it }
            Line("URL", url, KeyboardType.Uri) { url = it }
            Line("Tags (space separated)", tags) { tags = it }
            Line("Notes", notes) { notes = it }

            if (error.isNotEmpty()) {
                Text(error, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
            }

            Button(
                onClick = {
                    busy = true
                    scope.launch {
                        try {
                            val saved = VaultSession.put(
                                initial.copy(
                                    name = name.trim(),
                                    username = username,
                                    email = email,
                                    password = password,
                                    totp = totp.trim(),
                                    url = url,
                                    tags = tags.split(" ").filter { it.isNotBlank() },
                                    notes = notes,
                                )
                            )
                            onSaved(saved)
                        } catch (e: Exception) {
                            error = e.message ?: "Could not save"
                            busy = false
                        }
                    }
                },
                enabled = name.isNotBlank() && !busy,
                modifier = Modifier.fillMaxWidth(),
            ) { Text("Save") }
        }
    }
}

@Composable
private fun Line(
    label: String,
    value: String,
    keyboard: KeyboardType = KeyboardType.Text,
    onChange: (String) -> Unit,
) {
    OutlinedTextField(
        value = value,
        onValueChange = onChange,
        label = { Text(label) },
        singleLine = label != "Notes",
        keyboardOptions = KeyboardOptions(keyboardType = keyboard),
        visualTransformation = VisualTransformation.None,
        modifier = Modifier.fillMaxWidth(),
    )
}

// ------------------------------------------------------------- settings

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsScreen(onBack: () -> Unit) {
    val scope = rememberCoroutineScope()
    val snackbar = remember { SnackbarHostState() }
    var url by remember { mutableStateOf("") }
    var token by remember { mutableStateOf("") }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = { Text("Settings") },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.Default.ArrowBack, contentDescription = "Back")
                    }
                },
            )
        },
    ) { padding ->
        Column(
            Modifier
                .padding(padding)
                .padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Text("Sync", style = MaterialTheme.typography.titleMedium)
            OutlinedTextField(
                value = url,
                onValueChange = { url = it },
                label = { Text("Repository URL") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            Button(
                onClick = {
                    scope.launch {
                        val r = runCatching { VaultSession.setRemote(url.trim()) }
                        snackbar.showSnackbar(if (r.isSuccess) "Remote set" else "Failed: ${r.exceptionOrNull()?.message}")
                    }
                },
                enabled = url.isNotBlank(),
            ) { Text("Set remote") }

            OutlinedTextField(
                value = token,
                onValueChange = { token = it },
                label = { Text("HTTPS access token") },
                singleLine = true,
                visualTransformation = PasswordVisualTransformation(),
                modifier = Modifier.fillMaxWidth(),
            )
            Button(
                onClick = {
                    scope.launch {
                        val r = runCatching { VaultSession.setToken(token) }
                        snackbar.showSnackbar(if (r.isSuccess) "Token stored" else "Failed")
                        token = ""
                    }
                },
                enabled = token.isNotBlank(),
            ) { Text("Store token") }

            HorizontalDivider()

            Text("Maintenance", style = MaterialTheme.typography.titleMedium)
            Text(
                "Deleted entries leave a tombstone so the delete can reach your " +
                    "other devices. Collecting them early can resurrect entries on a " +
                    "device that has not synced recently.",
                style = MaterialTheme.typography.bodySmall,
            )
            Button(onClick = {
                scope.launch {
                    val r = runCatching { VaultSession.gc(90) }
                    snackbar.showSnackbar(
                        r.fold({ "Dropped $it tombstone(s)" }, { "Failed: ${it.message}" }),
                    )
                }
            }) { Text("Collect tombstones older than 90 days") }

            HorizontalDivider()
            Text(
                "Enable gitpass in Android Settings → Passwords & accounts → " +
                    "Autofill service to fill logins in other apps.",
                style = MaterialTheme.typography.bodySmall,
            )
        }
    }
}

/** Marks the clip sensitive so Android 13+ hides it from the clipboard preview. */
private fun copyToClipboard(context: Context, label: String, value: String) {
    val clipboard = context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
    val clip = ClipData.newPlainText(label, value).apply {
        description.extras = android.os.PersistableBundle().apply {
            putBoolean("android.content.extra.IS_SENSITIVE", true)
        }
    }
    clipboard.setPrimaryClip(clip)
}
