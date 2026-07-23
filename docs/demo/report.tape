# VHS tape that records the README plain-text report GIF.
#
# Regenerate with:
#   make build
#   go run docs/demo/server.go &            # start the local demo target
#   vhs docs/demo/report.tape               # writes docs/report.gif
#   kill %1                                  # stop the demo server
#
# Single-endpoint `-url` mode prints the report once at the end (no live
# TUI), so this is a brief run and then the real, colour-coded report holds
# on screen: status histogram + latency percentiles + distribution.

Output docs/report.gif

Set Shell "bash"
Set FontSize 13
Set Width 900
Set Height 760
Set Padding 16
Set Theme "Dracula"

Type "./bin/rkload -url http://127.0.0.1:8799/slower -c 80 -n 400"
Enter
Sleep 4s
Sleep 1500ms
