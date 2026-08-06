package com.gitpass.autofill

import android.content.Context
import android.service.autofill.Dataset
import android.service.autofill.FillResponse
import android.service.autofill.SaveInfo
import android.view.autofill.AutofillValue
import android.widget.RemoteViews
import com.gitpass.Entry

/**
 * Builds the [FillResponse] shown once the vault is unlocked: one row per
 * matching entry, plus a save rule so a newly typed login can be captured.
 */
object Responses {

    fun forEntries(context: Context, form: ParsedForm, entries: List<Entry>): FillResponse? {
        if (!form.isUsable) return null

        val builder = FillResponse.Builder()
        var rows = 0
        for (entry in entries.take(MAX_ROWS)) {
            builder.addDataset(dataset(context, form, entry) ?: continue)
            rows++
        }

        // Offer to save even when nothing matched — that is exactly the case
        // where the user is creating an account we do not know about yet.
        builder.setSaveInfo(saveInfo(form))

        return if (rows > 0 || form.passwordId != null) builder.build() else null
    }

    /** The response used while locked: one row that launches the unlock screen. */
    fun locked(context: Context, form: ParsedForm, sender: android.content.IntentSender): FillResponse? {
        if (!form.isUsable) return null
        val presentation = row(context, context.getString(com.gitpass.R.string.app_name), "Unlock to fill")
        return FillResponse.Builder()
            .setAuthentication(form.autofillIds, sender, presentation)
            .build()
    }

    private fun dataset(context: Context, form: ParsedForm, entry: Entry): Dataset? {
        val presentation = row(context, entry.name, entry.account.ifEmpty { "password only" })
        val builder = Dataset.Builder(presentation)
        var filled = false

        form.usernameId?.let { id ->
            val value = entry.account
            if (value.isNotEmpty()) {
                builder.setValue(id, AutofillValue.forText(value), presentation)
                filled = true
            }
        }
        form.passwordId?.let { id ->
            if (entry.password.isNotEmpty()) {
                builder.setValue(id, AutofillValue.forText(entry.password), presentation)
                filled = true
            }
        }
        return if (filled) builder.build() else null
    }

    private fun saveInfo(form: ParsedForm): SaveInfo {
        var type = 0
        if (form.usernameId != null) type = type or SaveInfo.SAVE_DATA_TYPE_USERNAME
        if (form.passwordId != null) type = type or SaveInfo.SAVE_DATA_TYPE_PASSWORD
        return SaveInfo.Builder(type, form.autofillIds).build()
    }

    private fun row(context: Context, title: String, subtitle: String): RemoteViews =
        RemoteViews(context.packageName, android.R.layout.simple_list_item_2).apply {
            setTextViewText(android.R.id.text1, title)
            setTextViewText(android.R.id.text2, subtitle)
        }

    /** Autofill dropdowns are small; more rows than this just hides the good ones. */
    private const val MAX_ROWS = 8
}
