package com.sixhobbits.ghostalert.ui

import android.widget.Toast
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.systemBarsPadding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.luminance
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.sixhobbits.ghostalert.Link
import com.sixhobbits.ghostalert.Repo
import com.sixhobbits.ghostalert.Snapshot
import com.sixhobbits.ghostalert.Tile

private val Background = Color(0xFF101014)
private val EmptyTile = Color(0xFF1A1A20)
private val Muted = Color(0xFF8A8A96)

@Composable
fun GhostAlertScreen() {
    MaterialTheme(colorScheme = darkColorScheme(background = Background, surface = Background)) {
        val snapshot by Repo.snapshot.collectAsStateWithLifecycle()
        val link by Repo.link.collectAsStateWithLifecycle()
        val error by Repo.lastError.collectAsStateWithLifecycle()
        var showSettings by remember { mutableStateOf(!Repo.configured) }

        Column(
            Modifier
                .fillMaxSize()
                .background(Background)
                .systemBarsPadding()
                .padding(10.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            StatusBar(
                link = link,
                snapshot = snapshot,
                error = error,
                onSettings = { showSettings = true },
            )
            if (snapshot == null) {
                Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                    Text(
                        if (Repo.configured) "waiting for the first update…"
                        else "tap the gear to point this at your Mac",
                        color = Muted,
                    )
                }
            } else {
                TileGrid(snapshot!!, Modifier.weight(1f))
            }
        }

        if (showSettings) {
            SettingsDialog(snapshot) { showSettings = false }
        }
    }
}

@Composable
private fun StatusBar(
    link: Link,
    snapshot: Snapshot?,
    error: String?,
    onSettings: () -> Unit,
) {
    Row(
        Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Box(
                Modifier
                    .clip(RoundedCornerShape(50))
                    .background(
                        when (link) {
                            Link.LIVE -> Color(0xFF22CC99)
                            Link.CONNECTING -> Color(0xFFFF9500)
                            Link.OFFLINE -> Color(0xFFDD1144)
                        }
                    )
                    .padding(5.dp)
            )
            Spacer(Modifier.padding(3.dp))
            Text(
                text = when (link) {
                    Link.LIVE -> snapshot?.host.orEmpty().ifBlank { "live" }
                    Link.CONNECTING -> "connecting…"
                    Link.OFFLINE -> error ?: "offline"
                },
                color = Muted,
                fontSize = 13.sp,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f, fill = false),
            )
        }
        TextButton(onClick = onSettings) { Text("⚙", color = Muted, fontSize = 18.sp) }
    }
}

@Composable
private fun TileGrid(snapshot: Snapshot, modifier: Modifier = Modifier) {
    // A fixed grid rather than a lazy one: every tile must be on screen at once,
    // which is the whole point of the layout.
    val tiles = snapshot.visible
    Column(modifier.fillMaxSize(), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        for (row in 0 until snapshot.rows) {
            Row(
                Modifier
                    .fillMaxWidth()
                    .weight(1f),
                horizontalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                for (col in 0 until snapshot.cols) {
                    val index = row * snapshot.cols + col
                    val tile = tiles.getOrNull(index)
                    Box(Modifier.weight(1f).fillMaxSize()) {
                        if (tile != null) TileView(tile)
                    }
                }
            }
        }
    }
}

@Composable
private fun TileView(tile: Tile) {
    if (tile.isEmpty) {
        Box(
            Modifier
                .fillMaxSize()
                .clip(RoundedCornerShape(14.dp))
                .background(EmptyTile),
            contentAlignment = Alignment.Center,
        ) {
            Text("${tile.slot}", color = Color(0xFF3A3A46), fontSize = 15.sp)
        }
        return
    }

    val context = LocalContext.current
    val background = remember(tile.color) { parseColor(tile.color) }
    val ink = if (background.luminance() > 0.45f) Color(0xFF14141A) else Color.White

    val accent = when (tile.state) {
        "waiting" -> Color(0xFFFF9500)
        "error" -> Color(0xFFFF3B30)
        "done" -> Color(0xFF34C759)
        "working" -> ink.copy(alpha = 0.35f)
        else -> Color.Transparent
    }

    // Waiting is the state you are meant to notice from across the room.
    val pulse = if (tile.state == "waiting") {
        val transition = rememberInfiniteTransition(label = "pulse")
        transition.animateFloat(
            initialValue = 1f,
            targetValue = 0.25f,
            animationSpec = infiniteRepeatable(tween(700), RepeatMode.Reverse),
            label = "pulseAlpha",
        ).value
    } else {
        1f
    }

    Column(
        Modifier
            .fillMaxSize()
            .clip(RoundedCornerShape(14.dp))
            .background(background)
            .border(3.dp, accent.copy(alpha = accent.alpha * pulse), RoundedCornerShape(14.dp))
            .clickable {
                // Touching a tile is a request to the Mac, so a failure has to
                // be visible: otherwise the tap just looks like it worked.
                Repo.focus(tile.slot) { result ->
                    result.onFailure {
                        Toast.makeText(context, it.message ?: "focus failed", Toast.LENGTH_SHORT)
                            .show()
                    }
                }
            }
            .padding(horizontal = 12.dp, vertical = 10.dp),
    ) {
        Text(
            tile.name,
            color = ink,
            fontWeight = FontWeight.Bold,
            fontSize = 17.sp,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        if (tile.message.isNotBlank()) {
            Text(
                tile.message,
                color = ink.copy(alpha = 0.75f),
                fontSize = 13.sp,
                maxLines = 3,
                overflow = TextOverflow.Ellipsis,
            )
        }
        Spacer(Modifier.weight(1f))
        Text(
            tile.state.uppercase(),
            color = ink,
            fontSize = 11.sp,
            fontWeight = FontWeight.Bold,
            modifier = Modifier
                .clip(RoundedCornerShape(50))
                .background(ink.copy(alpha = 0.12f))
                .padding(horizontal = 8.dp, vertical = 2.dp)
                .alpha(0.9f),
        )
    }
}

@Composable
private fun SettingsDialog(snapshot: Snapshot?, onClose: () -> Unit) {
    var host by remember { mutableStateOf(Repo.host) }
    var token by remember { mutableStateOf(Repo.token) }
    var cols by remember { mutableStateOf((snapshot?.cols ?: 2).toString()) }
    var rows by remember { mutableStateOf((snapshot?.rows ?: 5).toString()) }
    var result by remember { mutableStateOf<String?>(null) }

    AlertDialog(
        onDismissRequest = onClose,
        title = { Text("Mac connection") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(
                    value = host,
                    onValueChange = { typed ->
                        // Pasting the whole `ghostalert url` line fills in both
                        // fields rather than silently dropping the token.
                        val (address, pasted) = Repo.parseAddress(typed)
                        if (pasted.isNotBlank()) {
                            host = address
                            token = pasted
                        } else {
                            host = typed
                        }
                    },
                    label = { Text("Host or full URL") },
                    placeholder = { Text("192.168.1.71:7337") },
                    singleLine = true,
                )
                OutlinedTextField(
                    value = token,
                    onValueChange = { token = it },
                    label = { Text("Token") },
                    singleLine = true,
                )
                Text("Both are printed by `ghostalert url`.", color = Muted, fontSize = 12.sp)
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedTextField(
                        value = cols,
                        onValueChange = { cols = it.filter(Char::isDigit).take(1) },
                        label = { Text("Columns") },
                        singleLine = true,
                        modifier = Modifier.weight(1f),
                    )
                    OutlinedTextField(
                        value = rows,
                        onValueChange = { rows = it.filter(Char::isDigit).take(2) },
                        label = { Text("Rows") },
                        singleLine = true,
                        modifier = Modifier.weight(1f),
                    )
                }
                result?.let { Text(it, color = Muted, fontSize = 12.sp) }
            }
        },
        confirmButton = {
            Button(onClick = {
                Repo.probe(host, token) { probe ->
                    probe.onSuccess {
                        Repo.host = host
                        Repo.token = token
                        Repo.restart()
                        val c = cols.toIntOrNull() ?: it.cols
                        val r = rows.toIntOrNull() ?: it.rows
                        if (c != it.cols || r != it.rows) Repo.setGrid(c, r)
                        onClose()
                    }
                    probe.onFailure { error -> result = error.message ?: "could not connect" }
                }
            }) { Text("Save") }
        },
        dismissButton = { TextButton(onClick = onClose) { Text("Cancel") } },
    )
}

private fun parseColor(hex: String): Color = runCatching {
    Color(android.graphics.Color.parseColor(hex.ifBlank { "#dddddd" }))
}.getOrDefault(Color(0xFFDDDDDD))
