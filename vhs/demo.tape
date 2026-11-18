# Regenerate docs/images/demo.gif from the repository root:
#   make build && PATH="$PWD:$PATH" vhs vhs/demo.tape
# Without a local vhs, ttyd and Chromium:
#   make build && docker run --rm --user "$(id -u):$(id -g)" -e HOME=/tmp \
#     -v "$PWD":/vhs -v "$PWD/g5a":/usr/local/bin/g5a:ro \
#     ghcr.io/charmbracelet/vhs:v0.12.1 vhs/demo.tape
# Only the public, anonymous https://play.grafana.org instance is shown.
Output docs/images/demo.gif

Require g5a

# bash keeps the prompt clean; a personal zsh theme would end up in the GIF.
Set Shell bash
Set FontSize 16
Set Width 1280
Set Height 820
Set Padding 20
Set WindowBar Colorful
Set Theme "Catppuccin Frappe"
Set Margin 20
Set MarginFill "#1E1E2E"
Set BorderRadius 8
Set TypingSpeed 50ms
Set CursorBlink false

# Browse and filter dashboards
Type "g5a --url https://play.grafana.org"
Enter
Wait+Screen@30s /items/
Sleep 1.5s
Type "/"
Sleep 300ms
Type "cart service"
Sleep 800ms
Enter
Sleep 800ms
Enter

# Dashboard grid
Wait+Screen@30s /Dashboards > Cart Service/
Sleep 3s

# Switch the time range to the last 24 hours
Type "t"
Sleep 1.2s
Type "j"
Sleep 600ms
Enter
Sleep 4s

# Select the Request Rate panel and zoom it
Type "l"
Sleep 600ms
Type "l"
Sleep 1s
Enter
Wait+Screen@30s /Panel 3/
Sleep 2.5s

# Step to the latency percentiles panel
Type "j"
Sleep 1s
Type "j"
Wait+Screen@30s /Panel 5/
Sleep 3s

# Back to the grid, then scroll down
Escape
Sleep 2s
Type "j"
Sleep 700ms
Type "j"
Sleep 700ms
Type "j"
Sleep 700ms
Type "j"
Sleep 2.5s
