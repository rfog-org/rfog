# Changelog

All notable changes to RFoG. Dates are UTC. While RFoG is in alpha, rules,
numbers and ratings can change (or be reset) between versions.

## [Unreleased]

### Added
- **Long matches.** A second length next to the standard one: first to 10
  points over up to 20 turns (16 points in 3v3 to 5v5). In bot matches it
  runs about 14 turns instead of 8, with twice the ultimates and commanders
  reaching level 4 in about half the matches. Terminal, against bots or pass
  and play: `t` on the pick screen. On the web the bot card has a row for
  the length (standard, long). Online and rated play stay standard.
  `rfog balance -length long` measures the long length.
- **Team modes against bots, offline.** The experimental 2v2 to 5v5 modes
  could only be played online; `rfog bots` now offers them too (`m` on the
  pick screen), with bots beside you and against you. On the web the bot
  card has a row for the mode (1v1, 2v2 to 5v5), says what the choice means
  ("20 turns, first to 10"; "you and a bot ally against 2 bots on a 24×14
  board") and remembers it on the device. Play again keeps the mode and
  length. The turn counter, score bars, match point and the rules sheet read
  the match's own numbers.
- Web, team modes: your allies' figures are in your colour but dimmed (yours
  are bright), with a dark outline and a dashed ring at their feet; a tap
  shows one (and who orders it) but never takes it in hand. Callouts, the
  log, the strips and the end card speak of your team and the bots; the
  winner is your team or theirs. The bigger boards fit whole on a big
  screen, every square square.
- Web, team boards on a phone: a board whose squares would come out under
  about 27 px (22 px with a mouse) is shown up close (a 2v2 fills a phone's
  width, 28 to 31 px a square) and opens on your squad. A drag from a
  square with nothing to order pans, two fingers always do; a minimap in the
  corner shows the board, the fog, objectives, units and the view, and a tap
  on it goes there; a button in the corner (or a pinch) switches to the
  whole board and back. During playback the view glides to each cluster of
  events before they play, inside the same playback time (it cuts with
  reduced motion). Callouts stay in view. 1v1 is unchanged.
- Web: a pick screen. Against bots, pass and play and in an online draft,
  one screen shows a slot for every seat (your team on the left in your
  colour, theirs on the right) and all ten commanders below on the board's
  own colours. Tap one to read it, Lock in, and each seat fills as it
  picks with a flash; against bots the bots pick one after another (a tap
  skips). A commander already on your team is dimmed and cannot be picked;
  the two teams may share one. Online it shows the bans and the pick clock.
  The bot card keeps level, length and mode; Play again keeps your
  commander and goes straight to the bots' picks.
- Terminal: the pick screen is the web app's. A slot for every seat along
  the top (your team left, theirs right), all ten commanders in a grid of
  figures below (names on a small terminal), the chosen one's abilities and
  Lock in; then the bots' seats fill one by one. Arrows or `hjkl` move over
  the grid, `b` is the bot level, `m` and `t` are the mode and the length.
  The online draft is the same screen.
- Web, a dither pass in the board's own colours (pixel art's ordered 4×4
  dither, in steps): the fog is the board under its wall colour on a quarter
  of the pixels at the edge of your sight, half one square further, three
  quarters beyond (the terminal has the same three steps); the light under
  a selected unit, a cast's ring and the end card's pool fall off in steps;
  home, the bot card, the end card and the pick screen's floors are a
  stepped gradient between two of the board's colours, in big pixels the
  size of the figures'. Move dots have a faint light edge, to read on fog.
- Terminal: the same dither look, drawn with half-block pixels. Fog is the
  board's own squares under a pattern, thin at the edge of your sight and
  dense far from it, instead of a flat dark colour. Home, the pick screen
  and the end card sit on a dithered gradient in the board's colours.
- **Callouts.** The big moments are called out across the board in both
  apps: a commander down (on the terminal: enemy, yours, an ally's), an
  ultimate by name, an ultimate ready, the lead changing hands, match point.
  On the web the scorer's score jumps. On the terminal a hit shows its
  damage in large pixel digits on the flashing square; a killing blow is
  red on white.
- Terminal: the end card shows your commander, the result large, the score
  in pixel digits and the rating change; `enter` plays again (same mode,
  length and commanders), `q` goes to the menu.

### Changed
- Replays recorded before this version no longer play back: the rules
  changed (move then attack, ultimates at level 3), and under them an old
  replay showed a match that did not happen. They answer "recorded under
  older rules". The results and ratings of those matches are untouched.
- **Move, then attack.** An attack is now checked from where the unit ends its
  turn (its ordered move, or its own instant jump), not from where it starts.
  Both apps already drew the attack rings from the planned destination; the
  rules refused those attacks unless the target was also in reach before the
  move. A commander can now walk into range and shoot in the same turn. A
  target only in reach before the move can no longer be attacked with that
  move ordered.
- **Ultimates happen in 1v1.** `R` unlocks at level 3, and level 3 comes at
  3 XP (it was level 4 at 8 XP, which a 12-turn match never reached: bots
  cast none). `E` still unlocks at 2 XP and cooldowns still drop at 5 XP,
  which is now level 4. In bot matches about seven in ten now see an
  ultimate, the first around turn 6.
- **The hard bot is hard.** It played exactly as normal. It now tries eight
  plans each turn, plays each one a turn and a reply ahead on what it can
  see, and keeps the best. Against the normal bot with the same commander it
  wins about four matches in five. It sees no more than you do and rolls its
  own dice. A turn of thinking is about a tenth of a second in the browser.
- Balance, from 2700 bot matches a seed: Wren `def` 5 to 4 (won 65-71%, now
  54-57%), Hook cooldown 3 to 4 (Bram 58%, now 54-58%), Execute 5 to 4
  damage (Sable 61% once ultimates arrived, now 57-59%). The ten commanders
  sit between 37% and 59% in bot hands; it was 36% to 71%.
- A commander's level-up shows on the board in both apps: the level floats
  over it for a beat, with the key it unlocks (`L3 · R`) on the web board.
  It was a line in the log only.
- Terminal: a unit's move range is dots on the board's own squares, as on
  the web board, not a colour over the board: a large dot, light on dark
  squares and dark on light ones. The path to the cursor is a square marker,
  and an enemy the unit can hit stands on a dark red square. Figures have a
  line round them in their team's colour, their eyes glowing paler inside
  it.
- Terminal: a kill and an ultimate have the same weight. The figure holds on
  a white square for a beat, is struck out as the squares around take the
  shock (two deep for a commander), which fades over the next beats, dense,
  thinner, thin, then leaves dust. On a colour board an ultimate's ring is
  lit squares in the accent colour widening from the caster for two beats; a
  cast's target square lights the same way.
- Terminal without colour: portraits are the game's own figures drawn in
  characters, lights and visors in bold, in place of the placeholder set.
- Web: less work per tap. The board is measured once per redraw instead of
  about 130 times, and the squad list is rebuilt only when a unit in it
  changes.
- Web: a kill lands harder. The figure holds white for a beat before it
  breaks and the board jolts (more for a commander); an ultimate sends a wide
  ring across the board. The jolt is off with Ambient motion off or when the
  device asks for reduced motion.
- Web: damage numbers rise on the compositor instead of re-laying out.
- Web: every figure's eyes and lights glow in its team's colour, brighter
  when it attacks.
- Web: attacks land. A ranged shot flies as a bolt from a muzzle flash; a hit
  bursts in a flash and a star of light in the attacker's colour (bigger from
  a commander, whose melee strikes twice across) and knocks the target back
  the way the blow came. Damage numbers pop out large, white-hot on a killing
  blow; health bars drain, the light lag following. A miss only scatters a few
  pixels.
- Web: every tap is answered. The unit in hand stands up on a ring of light,
  its move dots and attack rings ripple out from it, an order lands with a
  ring where it will happen and its faded figure drops into place, Undo
  shrinks it away, and End turn glows once every unit has an order. Ending
  the turn sends a line of light up the board.
- Web: the end card shows your commander (in its attack pose on a win), the
  score, and an online rating counting up to its new value. Play again starts
  the same match in one tap: the same bot and commander, the same clock
  online after quick pairing, or pass and play.

### Fixed
- Online draft: the last pick shows before the board opens (the server
  sends the finished lineup; the page holds on it for a beat), and picks
  that arrive together, as the bots' do, show one by one.
- Bots on the same team no longer walk into the same square: in team modes
  about one move in four was two allies ordered onto one square, which stops
  both short. Each bot now leaves the squares its teammates are taking.
- Web: a unit's move dots and paths take your other planned moves into
  account: a path keeps off a square another of your units is ordered into
  where it can, so a unit stopped short does not land in a friend's square.
- Engine: an attack ordered together with a move or jump ending off the
  board (only possible with crafted orders) crashed the rules engine,
  offline and in the web app too. The attack is refused.
- Terminal: on a colour board, a cable under a marked square was drawn as a
  black bar across the square. It stays drawn now.
- `rfog -tier t1` and other flags with no command start the client, as the
  help says. They answered `unknown command`.
- Web: the ability sheet and the Learn page show each ability's full effect
  (damage, dice, turns, what it does to the target). A short stand-in had been
  shown instead.
- Bots play their whole kit. On normal they almost never cast a dozen
  abilities (Hook, Provoke, Jam, Ping, Spot, Relay, Fade, Phase, Arc Jump,
  Relocate, Whiteout, Flare); now they pull, taunt, silence, cloak, scout and
  jump when it pays, and Tally's Swarm is valued for all three junkbots.
  Expect a harder bot.
- Bots no longer plan an attack the rules refuse: about 1 planned order in 80
  was being dropped.
- Web: a commander with a jump ordered (Relocate, Phase, Arc Jump) shows as
  the faded figure where it will land, and its attack rings are drawn from
  there, as they already were for a walk.
- Web: playing the side that starts at the far edge (half of online matches),
  a melee lunge went away from its target and a shot recoiled toward it.
  Both follow the blow now.
- Server: a crafted set of orders could crash the whole server, ending every
  match on it. Orders off the board, or more orders than there are units, are
  refused; a bug hit while handling one request now answers that request with
  an error, and one hit while resolving a turn ends that match (unrated)
  instead of the server.
- Server: a draft pick arriving just as the draft finished could crash the
  server.
- Server: chat no longer passes control characters on, which a terminal ran as
  escape codes (moving the cursor, setting the title, writing the clipboard).
  Lines are cut at 200 bytes between characters, not inside one.
- Server: on a restart (every deploy), matches stop and save their turn before
  the database closes, and orders arriving during it are refused with "the
  server is restarting" instead of being lost.
- Server (self-hosting): limits against floods and idle sockets. A
  connection must sign in within 30 s; a client frame may be 64 KB; a
  connection's frames are paced to 10 a second after a burst of 40 (slowed
  down, not dropped); a client more than 8 MB behind on reading is dropped;
  four password checks run at once and the rest wait; a player may have 30
  games in progress (daily ones; two players queueing in a loop could open
  them without end); signed-out guests are no longer kept in memory until a
  restart; the web listener closes idle keep-alive connections after 2
  minutes and sends `nosniff`, `X-Frame-Options: SAMEORIGIN` and a referrer
  policy. Match ids are 64 random bits (32 would sooner or later repeat and
  write one match's result over another's).
- Rated matchmaking ignored ratings: the queue looked a rating up under the
  clock ("30s") while results are kept under its category ("blitz"), so
  everyone queued as 1500. It pairs by rating now, and two close players
  pair at once instead of waiting behind a far one who came first. The
  window still widens with waiting (150 points, then 10 more a second), so
  a small queue always pairs.
- Server: sign-in tokens are stored as a SHA-256 hash, so a copy of the
  database signs nobody in. Existing tokens are converted when the server
  starts, so players stay signed in, except that a sign-in not used for 90
  days expires, and from then on always does (the newest 20 per account are
  still kept).
- Server: "sign out of this device" from a client that sent no token could
  sign out another device that was signing in at the same moment. It now
  ends only the sign-in of the connection that asked.

### Removed
- The sprite sheet the web app fell back to (`sprites.png`), every stored
  portrait frame, the placeholder pack they were cut from and the tools that
  made them (`rfog asciify`, `tools/portraits`). Every client now draws every
  piece and portrait from the game's own figures.

## [v0.0.4] - 2026-11-06

### Fixed
- Web: units no longer blink or go invisible when you give an order or when
  they strike. Figures are drawn straight onto canvases instead of swapping
  images the browser had to decode.
- Web: an enemy walking out of the fog, or a unit spawned this turn, now shows
  during the turn's playback. They used to appear only when it ended.
- Web: effects no longer snap back into view after they finish, and a turn's
  last effects play out instead of being cut off when it ends.
- A turn's playback no longer eats your clock: every timed turn after the first
  gets a fixed 6 seconds on top, the same for both players, set by the server.

### Changed
- Web: smoother motion. Units glide along their whole path, stay the same
  element from turn to turn, and the board is only rebuilt when it changes.
  Costly filters and blend modes on the board are gone.
- Web: a long turn plays faster to stay near 5.5 seconds at normal speed.
- Casual games seat a bot after 15 seconds alone, down from 60.
- Balance: Wren's Tag gives 3 extra dice, up from 2. Over five seeds of bot
  matches Wren goes from 45.6% to 49.9%.
- Maps that are not symmetric under a 180-degree rotation are rejected when
  content loads (#9).

## [v0.0.3] - 2026-10-08

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
