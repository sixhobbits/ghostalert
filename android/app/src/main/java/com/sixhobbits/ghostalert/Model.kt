package com.sixhobbits.ghostalert

import org.json.JSONObject

/** One Ghostty tab, as shown on one tile. */
data class Tile(
    val slot: Int,
    val name: String,
    val state: String,
    val message: String,
) {
    val isEmpty: Boolean get() = state == "empty"
}

/** The whole grid as the daemon sees it. */
data class Snapshot(
    val rev: Long,
    val cols: Int,
    val rows: Int,
    val host: String,
    val tiles: List<Tile>,
) {
    /** Only the tiles that fit the grid, in slot order. */
    val visible: List<Tile> get() = tiles.take(cols * rows)

    companion object {
        fun parse(json: String): Snapshot {
            val o = JSONObject(json)
            val arr = o.optJSONArray("tiles")
            val tiles = buildList {
                for (i in 0 until (arr?.length() ?: 0)) {
                    val t = arr!!.getJSONObject(i)
                    add(
                        Tile(
                            slot = t.optInt("slot"),
                            name = t.optString("name"),
                            state = t.optString("state", "empty"),
                            message = t.optString("message"),
                        )
                    )
                }
            }
            return Snapshot(
                rev = o.optLong("rev"),
                cols = o.optInt("cols", 2).coerceAtLeast(1),
                rows = o.optInt("rows", 5).coerceAtLeast(1),
                host = o.optString("host"),
                tiles = tiles.sortedBy { it.slot },
            )
        }
    }
}

/** How the app is currently doing at talking to the daemon. */
enum class Link { OFFLINE, CONNECTING, LIVE }
