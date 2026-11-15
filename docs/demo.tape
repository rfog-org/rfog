# The terminal client: a match against the bots, two turns of orders.
# Needs VHS (github.com/charmbracelet/vhs). From the repo root:
#   go build -o rfog ./cmd/rfog && vhs docs/demo.tape
# The match seed is random, so every take differs: the bot's commander and
# the dice change, the orders below do not. VHS cannot keep up at more
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
# The game's own colours, so the window around the board matches it.
Set Theme { "name": "rfog", "black": "#0a0c10", "red": "#ff5a4a", "green": "#7ed98a", "yellow": "#e3c05c", "blue": "#4fd3e8", "magenta": "#b08ad6", "cyan": "#4fd3e8", "white": "#cdd5dc", "brightBlack": "#76808a", "brightRed": "#ff7043", "brightGreen": "#7ed98a", "brightYellow": "#e3c05c", "brightBlue": "#4fd3e8", "brightMagenta": "#b08ad6", "brightCyan": "#4fd3e8", "brightWhite": "#e6f0f5", "background": "#0a0c10", "foreground": "#cdd5dc", "selection": "#1b8ea3", "cursor": "#0a0c10" }
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
# The pick grid is five across: down from Hask, the default, is Sable, and
# two right of Sable is Volt. Enter locks in; the bot takes its seat and
# the match starts about a second later.
Type@120ms "jll"
Sleep 600ms
Enter
Sleep 2s

# Turn 1: into the middle. Enter confirms a move and picks up the next unit
# with no order, so there is no tab between them.
Type "m"
Sleep 250ms
Type@110ms "ll"
Sleep 150ms
Enter
Sleep 300ms
Type "m"
Sleep 250ms
Type@110ms "ll"
Sleep 150ms
Enter
Sleep 300ms
Type "m"
Sleep 250ms
Type@110ms "ll"
Sleep 150ms
Enter
Sleep 300ms
Type "m"
Sleep 250ms
Type@110ms "ll"
Sleep 150ms
Enter
Sleep 300ms
Type "m"
Sleep 250ms
Type@110ms "l"
Sleep 150ms
Enter
Sleep 300ms
Sleep 400ms
Type " "
Sleep 6s

# Turn 2: the commander, lineman, ranged and runner each hit what they can
# reach (the medic cannot attack).
Type "a"
Sleep 450ms
Enter
Sleep 300ms
Type "a"
Sleep 450ms
Enter
Sleep 300ms
Type "a"
Sleep 450ms
Enter
Sleep 300ms
Type "a"
Sleep 450ms
Enter
Sleep 300ms
Sleep 400ms
Type " "
Sleep 7s
