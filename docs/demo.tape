# The terminal client: a match against the bots, two turns of orders.
# Needs VHS (github.com/charmbracelet/vhs). From the repo root:
#   go build -o rfog ./cmd/rfog && vhs docs/demo.tape
# The match seed is random, so every take differs. VHS cannot keep up at more
# than about 7 fps at this size, and the clip then plays back too fast: check
# its length against the Sleeps below before using it.
Output demo.gif

Set Shell "bash"
Set FontFamily "Menlo"
Set FontSize 16
Set Width 1600
Set Height 900
Set Padding 0
Set Framerate 7
Set TypingSpeed 60ms

Hide
Type "export PATH=$PWD:$PATH HOME=$(mktemp -d) COLORTERM=truecolor TERM=xterm-256color PS1='$ '; clear"
Enter
Sleep 500ms
Show

Type "rfog bots -theme fibre"
Sleep 400ms
Enter
# The client queries the terminal as it starts: leave it a moment.
Sleep 2s
Type@120ms "jjjjjjjj"
Sleep 600ms
Enter
Sleep 1.2s

# Turn 1: into the middle.
Type "m"
Sleep 250ms
Type@110ms "ll"
Sleep 150ms
Enter
Sleep 250ms
Tab
Sleep 200ms
Type "m"
Sleep 250ms
Type@110ms "ll"
Sleep 150ms
Enter
Sleep 250ms
Tab
Sleep 200ms
Type "m"
Sleep 250ms
Type@110ms "ll"
Sleep 150ms
Enter
Sleep 250ms
Tab
Sleep 200ms
Type "m"
Sleep 250ms
Type@110ms "l"
Sleep 150ms
Enter
Sleep 250ms
Type " "
Sleep 4s

# Turn 2: everyone shoots the nearest target, if there is one.
Type "a"
Sleep 250ms
Enter
Sleep 200ms
Tab
Sleep 150ms
Type "a"
Sleep 250ms
Enter
Sleep 200ms
Tab
Sleep 150ms
Type "a"
Sleep 250ms
Enter
Sleep 200ms
Tab
Sleep 150ms
Type "a"
Sleep 250ms
Enter
Sleep 200ms
Type " "
Sleep 3s
