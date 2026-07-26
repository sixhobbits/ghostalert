# Hooks

`ghostalert-hook` pushes a tab's state to the daemon. It has no dependency on
any particular agent: wire it to whatever events the thing running in your tabs
emits.

```sh
ghostalert-hook --bind working        # started, and claim this tab's tile
ghostalert-hook waiting "approve?"    # blocked on a human
ghostalert-hook done                  # finished
ghostalert-hook error "build failed"  # broke
```

Given JSON on stdin it will use `.message` as the tile message, falling back to
the basename of `.cwd`. Otherwise pass the message as the second argument. It
always exits 0, so a stopped daemon cannot break the thing it is reporting on.

## Claiming a tile

`--bind` is what saves you registering every tab by hand. It works out which
tab you are in from which one is on screen, so use it only on an event that
means the human just acted in this tab — a prompt being submitted, a build
being started by hand. On a background event it would claim the wrong tile.

## Wiring examples

An agent that reads hook config as JSON, such as Claude Code:

```json
{
  "hooks": {
    "UserPromptSubmit": [
      { "hooks": [{ "type": "command", "command": "ghostalert-hook --bind working" }] }
    ],
    "Notification": [
      { "hooks": [{ "type": "command", "command": "ghostalert-hook waiting" }] }
    ],
    "Stop": [
      { "hooks": [{ "type": "command", "command": "ghostalert-hook done" }] }
    ]
  }
}
```

A shell script or Makefile target:

```sh
ghostalert-hook --bind working "tests"
if make test; then ghostalert-hook done "green"; else ghostalert-hook error "failed"; fi
```

A shell function that reports on anything slow you run by hand:

```sh
watched() {
  ghostalert-hook --bind working "$*"
  "$@" && ghostalert-hook done "$*" || ghostalert-hook error "$*"
}
```
