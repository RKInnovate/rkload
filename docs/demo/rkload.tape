# VHS tape that records the README TUI demo GIF.
#
# Regenerate with:
#   make build
#   go run docs/demo/server.go &            # start the local demo target
#   vhs docs/demo/rkload.tape               # writes docs/demo.gif
#   kill %1                                  # stop the demo server
#
# The dashboard renders 3 endpoints + a 2-step "checkout" scenario against
# the local server in docs/demo/server.go.

Output docs/demo.gif

Set Shell "bash"
Set FontSize 13
Set Width 1180
Set Height 640
Set Padding 16
Set Theme "Dracula"

Type "./bin/rkload -config docs/demo/demo.rkload.json"
Enter
Sleep 7s
Sleep 1500ms
