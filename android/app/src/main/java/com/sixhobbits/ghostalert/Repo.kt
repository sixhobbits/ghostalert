package com.sixhobbits.ghostalert

import android.content.Context
import android.util.Log
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject
import java.io.BufferedReader
import java.util.concurrent.TimeUnit

/**
 * Repo owns the single connection to the ghostalert daemon and the last grid it
 * sent. Both the activity and the foreground service read from here, so the
 * link survives the screen being turned off or the app being swiped away.
 */
object Repo {
    private const val TAG = "ghostalert"
    private const val PREFS = "ghostalert"
    private const val KEY_HOST = "host"
    private const val KEY_TOKEN = "token"

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val http = OkHttpClient.Builder()
        .connectTimeout(8, TimeUnit.SECONDS)
        // The event stream is open-ended, so it must never time out on read.
        .readTimeout(0, TimeUnit.MILLISECONDS)
        .retryOnConnectionFailure(true)
        .build()

    private lateinit var appContext: Context
    private var streamJob: Job? = null

    private val _snapshot = MutableStateFlow<Snapshot?>(null)
    val snapshot: StateFlow<Snapshot?> = _snapshot.asStateFlow()

    private val _link = MutableStateFlow(Link.OFFLINE)
    val link: StateFlow<Link> = _link.asStateFlow()

    private val _lastError = MutableStateFlow<String?>(null)
    val lastError: StateFlow<String?> = _lastError.asStateFlow()

    fun init(context: Context) {
        appContext = context.applicationContext
    }

    private val prefs get() = appContext.getSharedPreferences(PREFS, Context.MODE_PRIVATE)

    /** Base URL of the daemon, e.g. "http://192.168.1.71:7337". */
    var host: String
        get() = prefs.getString(KEY_HOST, "") ?: ""
        set(value) = prefs.edit().putString(KEY_HOST, normaliseHost(value)).apply()

    var token: String
        get() = prefs.getString(KEY_TOKEN, "") ?: ""
        set(value) = prefs.edit().putString(KEY_TOKEN, value.trim()).apply()

    val configured: Boolean get() = host.isNotBlank() && token.isNotBlank()

    /**
     * Splits what someone would actually paste into a base URL and, if it was
     * the whole line `ghostalert url` prints, the token from its `#t=`
     * fragment. A bare address gets the scheme and default port filled in.
     */
    fun parseAddress(raw: String): Pair<String, String> {
        var s = raw.trim()
        if (s.isEmpty()) return "" to ""
        // A ghostalert:// link carries the same thing the web URL does.
        if (s.startsWith("ghostalert://")) s = "http://" + s.removePrefix("ghostalert://")
        var found = ""
        val hash = s.indexOf("#t=")
        if (hash >= 0) {
            found = s.substring(hash + 3).trim()
            s = s.substring(0, hash)
        }
        if (!s.startsWith("http://") && !s.startsWith("https://")) s = "http://$s"
        s = s.trimEnd('/')
        if (!s.substringAfter("://").contains(':')) s = "$s:7337"
        return s to found
    }

    fun normaliseHost(raw: String): String = parseAddress(raw).first

    fun start() {
        if (streamJob?.isActive == true) return
        streamJob = scope.launch { streamForever() }
    }

    fun stop() {
        streamJob?.cancel()
        streamJob = null
        _link.value = Link.OFFLINE
    }

    /** Reconnect now, e.g. after the settings changed or the network came back. */
    fun restart() {
        stop()
        start()
    }

    private suspend fun streamForever() {
        var backoffMs = 1_000L
        while (scope.isActive) {
            if (!configured) {
                _link.value = Link.OFFLINE
                delay(2_000)
                continue
            }
            _link.value = Link.CONNECTING
            try {
                stream()
                backoffMs = 1_000L
            } catch (e: Exception) {
                Log.i(TAG, "stream ended: ${e.message}")
                _lastError.value = e.message
            }
            _link.value = Link.OFFLINE
            delay(backoffMs)
            backoffMs = (backoffMs * 2).coerceAtMost(15_000L)
        }
    }

    private fun stream() {
        val request = Request.Builder()
            .url("$host/api/events")
            .header("X-Ghostalert-Token", token)
            .header("Accept", "text/event-stream")
            .build()

        http.newCall(request).execute().use { response ->
            if (!response.isSuccessful) {
                throw IllegalStateException(
                    if (response.code == 401) "token rejected" else "HTTP ${response.code}"
                )
            }
            _lastError.value = null
            _link.value = Link.LIVE

            val reader = response.body?.charStream()?.buffered()
                ?: throw IllegalStateException("empty response")
            readEvents(reader)
        }
    }

    private fun readEvents(reader: BufferedReader) {
        val data = StringBuilder()
        while (true) {
            val line = reader.readLine() ?: throw IllegalStateException("stream closed")
            when {
                line.startsWith("data:") -> data.append(line.removePrefix("data:").trim())
                line.isEmpty() -> {
                    if (data.isNotEmpty()) {
                        runCatching { Snapshot.parse(data.toString()) }
                            .onSuccess { onSnapshot(it) }
                            .onFailure { Log.w(TAG, "bad payload", it) }
                        data.setLength(0)
                    }
                }
                // ":" comment lines are keep-alives, and "event:" is implied.
            }
        }
    }

    private fun onSnapshot(snap: Snapshot) {
        val previous = _snapshot.value
        _snapshot.value = snap
        Notifications.onSnapshot(appContext, previous, snap)
    }

    /** Ask the Mac to raise the Ghostty tab behind a tile. */
    fun focus(slot: Int, onResult: (Result<Unit>) -> Unit = {}) {
        scope.launch {
            val result = runCatching { post("/api/focus", JSONObject().put("slot", slot)) }
            withContext(Dispatchers.Main) { onResult(result.map { }) }
        }
    }

    /** Re-read the Mac's tab bar, picking up closed, renamed and new tabs. */
    fun refresh(onResult: (Result<Unit>) -> Unit = {}) {
        scope.launch {
            val result = runCatching { post("/api/refresh", JSONObject()) }
            withContext(Dispatchers.Main) { onResult(result.map { }) }
        }
    }

    /** Change the grid the daemon serves to every client. */
    fun setGrid(cols: Int, rows: Int, onResult: (Result<Unit>) -> Unit = {}) {
        scope.launch {
            val body = JSONObject().put("cols", cols).put("rows", rows)
            val result = runCatching { post("/api/grid", body) }
            withContext(Dispatchers.Main) { onResult(result.map { }) }
        }
    }

    /** One-shot fetch, used to check settings before saving them. */
    fun probe(candidateHost: String, candidateToken: String, onResult: (Result<Snapshot>) -> Unit) {
        scope.launch {
            val result = runCatching {
                val request = Request.Builder()
                    .url("${normaliseHost(candidateHost)}/api/state")
                    .header("X-Ghostalert-Token", candidateToken.trim())
                    .build()
                OkHttpClient.Builder()
                    .connectTimeout(5, TimeUnit.SECONDS)
                    .readTimeout(5, TimeUnit.SECONDS)
                    .build()
                    .newCall(request).execute().use { response ->
                        val body = response.body?.string().orEmpty()
                        if (!response.isSuccessful) {
                            throw IllegalStateException(
                                if (response.code == 401) "token rejected" else "HTTP ${response.code}"
                            )
                        }
                        Snapshot.parse(body)
                    }
            }
            withContext(Dispatchers.Main) { onResult(result) }
        }
    }

    private fun post(path: String, body: JSONObject): String {
        val request = Request.Builder()
            .url("$host$path")
            .header("X-Ghostalert-Token", token)
            .post(body.toString().toRequestBody("application/json".toMediaType()))
            .build()
        return OkHttpClient.Builder()
            .connectTimeout(5, TimeUnit.SECONDS)
            .readTimeout(20, TimeUnit.SECONDS)
            .build()
            .newCall(request).execute().use { response ->
                val text = response.body?.string().orEmpty()
                if (!response.isSuccessful) throw IllegalStateException(errorOf(text, response.code))
                text
            }
    }

    private fun errorOf(body: String, code: Int): String =
        runCatching { JSONObject(body).getString("error") }.getOrElse { "HTTP $code" }
}
