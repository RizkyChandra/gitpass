package com.gitpass.autofill

import android.app.assist.AssistStructure
import android.text.InputType
import android.view.View
import android.view.autofill.AutofillId

/** The fillable fields found in a screen, plus what app or site they belong to. */
data class ParsedForm(
    val usernameId: AutofillId? = null,
    val passwordId: AutofillId? = null,
    val target: Target = Target(),
) {
    val isUsable: Boolean get() = usernameId != null || passwordId != null

    val autofillIds: Array<AutofillId>
        get() = listOfNotNull(usernameId, passwordId).toTypedArray()
}

/**
 * Walks the view tree looking for a username and a password field.
 *
 * Explicit autofill hints are trusted first. Most apps and pages set none, so
 * there is a fallback over input types and the id/hint text — that is the only
 * way to be useful on the majority of real screens.
 */
object StructureParser {

    fun parse(structure: AssistStructure): ParsedForm {
        var username: AutofillId? = null
        var password: AutofillId? = null
        var webDomain = ""

        fun visit(node: AssistStructure.ViewNode) {
            node.webDomain?.takeIf { it.isNotEmpty() }?.let { if (webDomain.isEmpty()) webDomain = it }

            val id = node.autofillId
            if (id != null && node.autofillType == View.AUTOFILL_TYPE_TEXT) {
                when (classify(node)) {
                    Field.PASSWORD -> if (password == null) password = id
                    Field.USERNAME -> if (username == null) username = id
                    Field.NONE -> Unit
                }
            }
            for (i in 0 until node.childCount) visit(node.getChildAt(i))
        }

        for (i in 0 until structure.windowNodeCount) {
            visit(structure.getWindowNodeAt(i).rootViewNode)
        }

        return ParsedForm(
            usernameId = username,
            passwordId = password,
            target = Target(packageName = structure.activityComponent?.packageName ?: "", webDomain = webDomain),
        )
    }

    private enum class Field { USERNAME, PASSWORD, NONE }

    private fun classify(node: AssistStructure.ViewNode): Field {
        node.autofillHints?.forEach { raw ->
            when (raw.lowercase()) {
                View.AUTOFILL_HINT_PASSWORD, "password" -> return Field.PASSWORD
                View.AUTOFILL_HINT_USERNAME, "username",
                View.AUTOFILL_HINT_EMAIL_ADDRESS, "email", "tel" -> return Field.USERNAME
            }
        }

        // No hints: fall back to the input type, which is reliable for
        // passwords because the platform needs it to mask the text.
        val type = node.inputType
        val variation = type and InputType.TYPE_MASK_VARIATION
        if (type and InputType.TYPE_MASK_CLASS == InputType.TYPE_CLASS_TEXT) {
            when (variation) {
                InputType.TYPE_TEXT_VARIATION_PASSWORD,
                InputType.TYPE_TEXT_VARIATION_VISIBLE_PASSWORD,
                InputType.TYPE_TEXT_VARIATION_WEB_PASSWORD,
                -> return Field.PASSWORD

                InputType.TYPE_TEXT_VARIATION_EMAIL_ADDRESS,
                InputType.TYPE_TEXT_VARIATION_WEB_EMAIL_ADDRESS,
                -> return Field.USERNAME
            }
        }

        // Last resort: what the field calls itself.
        val words = listOfNotNull(node.idEntry, node.hint, node.contentDescription?.toString())
            .joinToString(" ").lowercase()
        return when {
            words.isEmpty() -> Field.NONE
            words.contains("password") || words.contains("passwd") || words.contains("pwd") -> Field.PASSWORD
            words.contains("email") || words.contains("username") ||
                words.contains("user") || words.contains("login") || words.contains("account") -> Field.USERNAME

            else -> Field.NONE
        }
    }
}
