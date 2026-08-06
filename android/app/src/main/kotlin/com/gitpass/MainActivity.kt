package com.gitpass

import android.os.Bundle
import android.view.WindowManager
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.graphics.Color

/** Where the user currently is. A nav library would be overkill for five screens. */
sealed interface Screen {
    data object Setup : Screen
    data object Unlock : Screen
    data object List : Screen
    data class Detail(val id: String) : Screen
    data class Edit(val entry: Entry) : Screen
    data object Settings : Screen
}

class MainActivity : ComponentActivity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        // Keep passwords out of screenshots and the recents thumbnail.
        window.setFlags(WindowManager.LayoutParams.FLAG_SECURE, WindowManager.LayoutParams.FLAG_SECURE)

        setContent {
            GitpassTheme {
                Surface {
                    App()
                }
            }
        }
    }
}

@Composable
private fun GitpassTheme(content: @Composable () -> Unit) {
    val dark = isSystemInDarkTheme()
    val scheme = if (dark) {
        darkColorScheme(primary = Color(0xFFC8B6FF), surface = Color(0xFF1F1B2E))
    } else {
        lightColorScheme(primary = Color(0xFF5B3FBF))
    }
    MaterialTheme(colorScheme = scheme, content = content)
}

@Composable
private fun App() {
    val context = androidx.compose.ui.platform.LocalContext.current
    var screen: Screen by remember {
        mutableStateOf(
            when {
                VaultSession.isUnlocked -> Screen.List
                VaultSession.exists(context) -> Screen.Unlock
                else -> Screen.Setup
            }
        )
    }

    when (val s = screen) {
        is Screen.Setup -> SetupScreen(onReady = { screen = Screen.List })

        is Screen.Unlock -> UnlockScreen(
            onUnlocked = { screen = Screen.List },
        )

        is Screen.List -> ListScreen(
            onOpen = { screen = Screen.Detail(it.id) },
            onAdd = { screen = Screen.Edit(Entry()) },
            onSettings = { screen = Screen.Settings },
            onLock = {
                VaultSession.lock()
                screen = Screen.Unlock
            },
        )

        is Screen.Detail -> DetailScreen(
            id = s.id,
            onBack = { screen = Screen.List },
            onEdit = { screen = Screen.Edit(it) },
            onDeleted = { screen = Screen.List },
        )

        is Screen.Edit -> EditScreen(
            initial = s.entry,
            onCancel = {
                screen = if (s.entry.id.isEmpty()) Screen.List else Screen.Detail(s.entry.id)
            },
            onSaved = { screen = Screen.Detail(it.id) },
        )

        is Screen.Settings -> SettingsScreen(onBack = { screen = Screen.List })
    }
}
