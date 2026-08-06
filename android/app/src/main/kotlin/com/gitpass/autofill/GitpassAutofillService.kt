package com.gitpass.autofill

import android.app.PendingIntent
import android.app.assist.AssistStructure
import android.content.Intent
import android.os.CancellationSignal
import android.service.autofill.AutofillService
import android.service.autofill.FillCallback
import android.service.autofill.FillRequest
import android.service.autofill.SaveCallback
import android.service.autofill.SaveRequest
import android.util.Log
import android.view.autofill.AutofillId
import com.gitpass.Entry
import com.gitpass.VaultSession

/**
 * Fills logins from the vault into other apps and browsers.
 *
 * The vault is normally locked, so the common path is to answer with an
 * authentication request; Android then launches [AutofillUnlockActivity],
 * which unlocks and returns the real datasets.
 */
class GitpassAutofillService : AutofillService() {

    override fun onFillRequest(
        request: FillRequest,
        cancellationSignal: CancellationSignal,
        callback: FillCallback,
    ) {
        val structure = request.fillContexts.lastOrNull()?.structure
        if (structure == null) {
            callback.onSuccess(null)
            return
        }

        val form = StructureParser.parse(structure)
        if (!form.isUsable) {
            callback.onSuccess(null)
            return
        }

        if (!VaultSession.isUnlocked) {
            callback.onSuccess(Responses.locked(this, form, unlockSender()))
            return
        }

        try {
            val matches = matchEntries(VaultSession.listBlocking(), form.target)
            callback.onSuccess(Responses.forEntries(this, form, matches))
        } catch (e: Exception) {
            // A failure here must not surface as a crash in the app being
            // filled; degrade to "no suggestions".
            Log.w(TAG, "fill failed", e)
            callback.onSuccess(null)
        }
    }

    override fun onSaveRequest(request: SaveRequest, callback: SaveCallback) {
        val structure = request.fillContexts.lastOrNull()?.structure
        if (structure == null) {
            callback.onFailure("Nothing to save")
            return
        }
        if (!VaultSession.isUnlocked) {
            callback.onFailure("Unlock gitpass first, then save again")
            return
        }

        val form = StructureParser.parse(structure)
        val values = readValues(structure)
        val username = form.usernameId?.let { values[it] }.orEmpty()
        val password = form.passwordId?.let { values[it] }.orEmpty()
        if (password.isEmpty()) {
            callback.onFailure("No password field found")
            return
        }

        try {
            val entry = Entry(
                name = form.target.label.ifEmpty { "untitled" },
                username = username,
                password = password,
                url = form.target.webDomain,
            )
            VaultSession.putBlocking(entry)
            callback.onSuccess()
        } catch (e: Exception) {
            Log.w(TAG, "save failed", e)
            callback.onFailure("Could not save: ${e.message}")
        }
    }

    /** Collects the text the user actually typed, keyed by field. */
    private fun readValues(structure: AssistStructure): Map<AutofillId, String> {
        val out = mutableMapOf<AutofillId, String>()
        fun visit(node: AssistStructure.ViewNode) {
            val id = node.autofillId
            val value = node.autofillValue
            if (id != null && value != null && value.isText) {
                out[id] = value.textValue.toString()
            }
            for (i in 0 until node.childCount) visit(node.getChildAt(i))
        }
        for (i in 0 until structure.windowNodeCount) visit(structure.getWindowNodeAt(i).rootViewNode)
        return out
    }

    private fun unlockSender() = PendingIntent.getActivity(
        this,
        UNLOCK_REQUEST,
        Intent(this, AutofillUnlockActivity::class.java),
        // Mutable because the platform adds the assist structure to this intent
        // before launching it.
        PendingIntent.FLAG_MUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
    ).intentSender

    private companion object {
        const val TAG = "GitpassAutofill"
        const val UNLOCK_REQUEST = 1001
    }
}
