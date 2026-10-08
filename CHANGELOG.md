# Changelog

All notable changes to RFoG. Dates are UTC. While RFoG is in alpha, rules,
numbers and ratings can change (or be reset) between versions.

## 0.0.3 (unreleased)

The first version anyone else plays: the whole game, in a browser and in a
terminal. (0.0.2 was the first tagged build: the launch code plus the first
two contributions.)

### The game
- WEGO squad tactics: both sides plan in secret, then the turn resolves at
  once (instant abilities, delayed abilities, movement, overwatch, attacks,
  scoring), with initiative, height, cover, line of sight and fog.
- An 8×8 board for 1v1 with a raised core and two side objectives.
  Experimental team modes (2v2 to 5v5) in the terminal client, not yet
  balanced.
- Net scoring: each turn the side holding more objectives scores the
  difference; a commander kill is worth 2; first to 5, or the lead after 12
  turns.
- **Ten commanders**, four abilities each, unlocking with level: HASK
  (breaker), WREN (marksman), NUL (operator), MOTT (signaler), TALLY
  (summoner), VOLT (lancer), KITE (spotter), BRAM (warden), SABLE
  (infiltrator), ORDO (bombard). Plus four squad units (lineman, ranged,
  runner, medic) and two summons (junkbot, drone).
- Balance measured with bot-vs-bot play: in 1v1 every commander wins 43–55%
  and neither side has an edge.
- Every rule, hero, ability and map is data (`data/*.toml`).

### Web app
- The graphical board, its engine running in the page as WebAssembly: tap a
  unit for its moves (dots) and targets (rings, with the dice), tap or drag to
  give orders.
- Original pixel figures with team-coloured eyes and lights, idle animation,
  attack poses, and effects drawn from what each ability does: lunges and
  slashes, beams, lightning, lobbed orbs, blasts, radar rings, smoke, chains,
  shattering deaths. Playback speed, skip, and motion off are settings.
- A board that reads like infrastructure: fibre runs under the floor, wall
  blocks, ledges on high ground, objectives that light up for whoever holds
  them, a forecast of who scores next turn.
- A front page in the style of a chess site: quick pairing grid, rated or
  casual, play against bots, challenge a friend, your games and the
  leaderboard, Learn, and a preferences page.
- Opens straight into play, as a chess site does: an anonymous guest until
  you sign in. **Watch** live matches; **Pass and play** for two players at
  one device.
- Daily matches: a move a day, listed under Your games until they end.
- Offline games against bots resume after a reload.
- Installable on a phone's home screen.

### Terminal
- Plays everything by keyboard (mouse and touch also work), from 80×24
  monochrome up to truecolor; the same pixel figures in half blocks on big
  tiles, damage and healing numbers, fibre runs under the floor.
- Ten boards, the same in the terminal and the web app (fibre, graphite,
  daylight, abyss, nord, gruvbox, catppuccin, tokyo-night, amber, green;
  tokyo-night from a contributor's pull request), each readable
  at 24-bit and 256 colours; plus mono for terminals without colour. A
  custom `~/.config/rfog/theme.toml` makes a board from `light`, `dark` and
  `wall`.
- A home screen laid out like the web app's (quick pairing grid, rated or
  casual, side column, your games, leaderboard, and the top bar: Watch,
  Learn, Leaderboard) on big terminals; a list on small ones. Back from an
  online screen returns there.
- Tutorial, hotseat, roster, replays, settings, a command line (`:`).

### Online
- One server for every client at one address: the web app and the game
  protocol over HTTPS (the terminal client connects the same way), SSH play,
  and plain TCP for self-hosters.
- Nine clocks: 10s, 15s (bullet), 20s, 30s (blitz), 45s, 60s (rapid), 90s+,
  120s+ (long-haul, with time banks), 24h (daily), each **rated or casual**.
  Glicko-2 ratings per category. Casual games welcome guests and fill empty
  seats with bots after a wait.
- Draft with bans (each player bans one commander, then picks).
- **Challenge a friend** by link (web) or code (terminal); the codes work in
  both. Challenges are casual: ratings come from pairing only.
- **Resume anywhere, play in one place:** a match follows the account between
  the web app and the terminal; opening the game shows a match in progress
  (or how the last one went). A second device takes the match over; the first
  is told.
- Spectating (ranked matches a turn behind), chat, match history, replays,
  leaderboards.

### Accounts
- Guests welcome: just a name. An account is a name and a password; a guest
  can save its progress as one at any time.
- No email: a recovery code is shown once and can reset the password.
- Signed in on several devices at once; sign out here or everywhere; delete
  the account (and everything tied to it).
- Passwords and recovery codes are stored as argon2id hashes; repeated
  failed logins are throttled.

### License
- GNU AGPL v3. The name and logo are reserved (TRADEMARKS.md).

### Self-hosting
- One static binary for Linux, macOS, FreeBSD and Windows; SQLite or
  Postgres; TLS; the rules travel to clients when they differ from the
  client's own.
- Counts for the operator, with nothing kept about any player: the server
  logs a `stats` line every 10 minutes (online, peak, by client, matches),
  and `rfog stats` prints accounts, guests, online matches and players per
  day from the store.
