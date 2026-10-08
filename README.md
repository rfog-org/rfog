<p align="center"><img src="web/icon.svg" width="88" alt="RFoG logo"></p>

<h1 align="center">RFoG</h1>

<p align="center"><b>RF over Glass</b><br>
Squad tactics where both sides plan at once.<br>
Free and open source. Play in the browser or in a terminal, on one account.</p>

<p align="center"><a href="https://rfog.org"><b>Play at rfog.org</b></a> · <code>ssh -p 2222 yourname@rfog.org</code></p>

<p align="center">
<a href="LICENSE"><img alt="License: AGPL-3.0" src="https://img.shields.io/badge/license-AGPL--3.0-4fd3e8"></a>
<img alt="Status: alpha" src="https://img.shields.io/badge/status-alpha-e3c05c">
<img alt="Go 1.25+" src="https://img.shields.io/badge/go-1.25%2B-76808a">
</p>

<p align="center">
<img src="https://raw.githubusercontent.com/rfog-org/.github/main/assets/screenshots/web-phone-home.jpg" width="31%" alt="The web app on a phone: quick pairing by clock, bots, pass and play, challenges">
<img src="https://raw.githubusercontent.com/rfog-org/.github/main/assets/screenshots/web-phone-match.jpg" width="31%" alt="A match on a phone: the squad holding a contested objective">
<img src="https://raw.githubusercontent.com/rfog-org/.github/main/assets/screenshots/web-phone-replay.jpg" width="31%" alt="A replay on a phone: an ability's blast and the damage it does">
</p>

## About

RFoG is a competitive turn-based tactics game. Each player leads a commander
and four units across a small board with high ground, cover and fog, fighting
over signal nodes. Both sides give their orders in secret, then the turn
resolves for everyone at once. A match takes 10 to 15 minutes.

There is nothing to buy and nothing to unlock. Every commander is available
to every player. No ads and no paid features, on the Lichess model.

### Features

- **Simultaneous turns.** Orders are planned in secret and resolved together
  in a fixed order: instant abilities, delayed abilities, movement,
  overwatch, attacks, scoring.
- **1v1, ten commanders.** Each commander has four abilities; both players
  ban one, then pick.
- **Two clients, one game.** A graphical web app for desktop and phone, and a
  terminal client that runs natively or over SSH with nothing to install.
  A match started on one continues on the other.
- **Chess-style clocks.** Nine time controls from 10 seconds a turn to a day
  a turn, each rated or casual. Glicko-2 ratings per category.
- **Offline play** against bots at three levels, or pass and play on one
  device. No account needed.
- **Online play:** quick pairing, challenges by link or code, live matches to
  watch, replays of every game, leaderboards.
- **Private accounts.** Play as a guest straight away. An account is a name
  and a password, with no email and no tracking. A one-time recovery code
  replaces password reset by email.
- **Fair by construction.** The server runs every match and each client sees
  only what its units see. Maps are mirror-symmetric and the rules are
  tested for side bias.
- **Self-hostable.** One static binary and one SQLite file. Every rule, unit
  and map is a TOML file.

## The terminal client

<p align="center">
<img src="https://raw.githubusercontent.com/rfog-org/.github/main/assets/screenshots/terminal-match.png" width="88%" alt="A match in the terminal client: the board with pixel figures, the unit panel and the squad">
</p>

<p align="center">
<img src="https://raw.githubusercontent.com/rfog-org/.github/main/assets/screenshots/terminal-home.png" width="49%" alt="The terminal home screen: quick pairing, games and leaderboard">
<img src="https://raw.githubusercontent.com/rfog-org/.github/main/assets/screenshots/terminal-roster.png" width="49%" alt="The commander roster in the terminal">
</p>

The terminal client draws the same pixel figures as the web app in half
blocks and plays by keyboard, mouse or touch. It adapts from an 80×24
monochrome terminal up to full colour. Terminals with 24-bit colour (iTerm2,
Ghostty, WezTerm, kitty) show the board as in these captures; others use
the nearest 256-colour palette.

## Play

**In a browser:** open <https://rfog.org>, or a server's own address
(<http://localhost:8080/> for a local one). Bots, pass and play and the tutorial work without signing
in. Online play starts as a guest.

**In a terminal:** download the archive for your system from the
[releases](https://github.com/rfog-org/rfog/releases), unpack it and run
`rfog`, or build it:

```sh
go build -o rfog ./cmd/rfog         # Go 1.25 or newer
./rfog                              # the home screen
./rfog bots                         # straight into a match against a bot
./rfog online                       # online, on rfog.org
ssh -p 2222 yourname@rfog.org       # or play over SSH, nothing to install
```

New players can start with the tutorial (web: **Learn**, terminal: **How
to play**), a guided first match. The [manual](docs/MANUAL.md) covers the
turn, scoring, keys, every commander and online play.

## Host your own

```sh
./rfog serve        # TCP :7777, SSH :2222, web :8080, sqlite://rfog.db
./rfog serve -tcp :7777 -ssh "" -ws :8080 -db sqlite://data/rfog.db
```

One binary with no other services. It serves the web app at `/` and the game
protocol at `/net` (browsers, and terminal clients given `-server
example.org`), native clients over TCP, and SSH sessions.

| Flag | Purpose |
|---|---|
| `-tls-cert`, `-tls-key` | TLS for the TCP listener (clients use `tls://host:port`) |
| `-db postgres://...` | Postgres instead of SQLite (same schema, migrations run on start) |
| `-ws-origins` | Lets a web app hosted on another site connect |
| `-motd` | Message of the day |

Every flag has an `RFOG_*` environment variable, so a systemd unit or a
container needs no arguments. The rules in [`data/`](data/) are sent to any
client whose copy differs, so an edited TOML file applies to everyone who
connects.

## Build and test

To work on it, with Go 1.25 or newer:

```sh
git clone https://github.com/rfog-org/rfog.git && cd rfog
go test ./...              # the tests
go build -o rfog ./cmd/rfog
./rfog serve               # a local server; the web app at http://localhost:8080/
```

Then, from the repository:

```sh
make                       # vet, test, build
make web                   # rebuild web/engine.wasm after engine or data changes
make cross                 # static binaries for Linux, macOS, FreeBSD and Windows
make package VERSION=v0.1.0  # release archives and SHA256SUMS in dist/
./rfog balance -n 15       # bot-vs-bot win rates per commander
./rfog replay file.json    # watch a saved replay
```

The engine is pure Go with no dependencies and compiles to WebAssembly, so
the browser, the terminal and the server run the same rules.
[CONTRIBUTING](docs/CONTRIBUTING.md) explains the layout, the tests and how
to change balance.

## Status

**Alpha.** 1v1 is complete and playable. Numbers are still being
tuned, and ratings may be reset before the beta. The
[changelog](CHANGELOG.md) lists what each version contains.

Bug reports and feedback are welcome in the issues.

## License

RFoG is free software under the [GNU AGPL v3](LICENSE). You may use, change
and host it. If you run a modified version for others, you must share your
changes with them. The RFoG name and logo are not covered by the license;
see [TRADEMARKS](TRADEMARKS.md).

## Credits

The commanders and units are original pixel figures. The fallback sprites
and monochrome portraits are from [Tiny Dungeon](https://kenney.nl/assets/tiny-dungeon)
by Kenney (CC0). Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea),
[lipgloss](https://github.com/charmbracelet/lipgloss) and
[wish](https://github.com/charmbracelet/wish).
