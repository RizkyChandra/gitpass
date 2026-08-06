package com.gitpass.autofill

import android.app.assist.AssistStructure
import android.content.Intent
import android.os.Bundle
import android.view.WindowManager
import android.view.autofill.AutofillManager
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.lifecycle.lifecycleScope
import com.gitpass.VaultSession
import kotlinx.coroutines.launch

/**
 * Shown when autofill is requested against a locked vault. It unlocks, builds
 * the datasets, and hands them straight back to the platform — the main UI
 * never appears.
 */
class AutofillUnlockActivity : ComponentActivity() {

    private lateinit var form: ParsedForm

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        // Passwords are about to be on screen.
        window.setFlags(WindowManager.LayoutParams.FLAG_SECURE, WindowManager.LayoutParams.FLAG_SECURE)

        @Suppress("DEPRECATION")
        val structure: AssistStructure? =
            intent.getParcelableExtra(AutofillManager.EXTRA_ASSIST_STRUCTURE)
        if (structure == null) {
            setResult(RESULT_CANCELED)
            finish()
            return
        }
        form = StructureParser.parse(structure)

        setContent {
            MaterialTheme {
                Surface {
                    UnlockPrompt(
                        title = form.target.label.ifEmpty { "gitpass" },
                        onUnlock = ::unlockAndReply,
                    )
                }
            }
        }
    }

    /** Unlocks, then returns the datasets through the autofill result extra. */
    private fun unlockAndReply(passphrase: String, onError: (String) -> Unit) {
        lifecycleScope.launch {
            try {
                if (!VaultSession.isUnlocked) {
                    VaultSession.unlock(this@AutofillUnlockActivity, passphrase)
                }
                val matches = matchEntries(VaultSession.list(), form.target)
                val response = Responses.forEntries(this@AutofillUnlockActivity, form, matches)
                setResult(
                    RESULT_OK,
                    Intent().putExtra(AutofillManager.EXTRA_AUTHENTICATION_RESULT, response),
                )
                finish()
            } catch (e: Exception) {
                onError(e.message ?: "Wrong passphrase")
            }
        }
    }
}

@Composable
private fun UnlockPrompt(title: String, onUnlock: (String, (String) -> Unit) -> Unit) {
    var passphrase by remember { mutableStateOf("") }
    var error by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }

    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(title, style = MaterialTheme.typography.titleMedium)
        Text("Unlock your vault to fill this login.", style = MaterialTheme.typography.bodySmall)
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
                onUnlock(passphrase) { message -> error = message; busy = false }
            },
            enabled = passphrase.isNotEmpty() && !busy,
            modifier = Modifier.fillMaxWidth(),
        ) {
            Text(if (busy) "Unlocking…" else "Unlock")
        }
    }
}
