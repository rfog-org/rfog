// The graphical client: draws the board and turns taps and drags into
// orders. Every rule — where a unit can go, what it can hit, whether an
// order is legal, what happens when the turn resolves — is decided by the
// engine, compiled to WebAssembly (engine.wasm) and running in this page,
// so a tap is answered at once. The same engine runs on the server and in
// the terminal client.
(function () {
	'use strict';

	var $ = function (id) { return document.getElementById(id); };
	var G = null;          // the engine (window.rfogGame)
	var C = null;          // content: heroes, units, abilities, rules
	var ART = null;        // pixel figures: { size, palette, units, floating, poses }
	// Team colour and shade: yours, theirs, and an ally's (your colour, but
	// outlined dark rather than lit: on your side, not yours to order).
	var TEAM = [['#4fd3e8', '#2a8fa0'], ['#ff7043', '#b84a2a'], ['#4fd3e8', '#2a8fa0', '#14424b']];

	// ---- engine calls: JSON in, JSON out --------------------------------
	function call(name) {
		var args = Array.prototype.slice.call(arguments, 1);
		var raw;
		try { raw = G[name].apply(null, args); } catch (e) { lost(e); throw e; }
		var out = JSON.parse(raw);
		if (out && out.error) { throw new Error(out.error); }
		return out;
	}
	// lost is the engine gone (it cannot be restarted in place): say so and
	// offer a reload, which resumes the saved match.
	function lost(e) {
		$('loading').innerHTML = '<h1 class="wordmark">RFoG</h1><p class="tag">rf over glass</p><small>the engine stopped (' + (e && e.message || e) + ')</small>' +
			'<button class="primary" onclick="location.reload()">Reload</button>';
		show('loading');
	}

	// ---- keeping the match: a phone drops background tabs, and a chess app
	// comes back to the game. Storage can be missing or full; play goes on.
	var SAVE = 'rfog.game', ORDERS = 'rfog.orders';
	function keep(k, v) { try { if (v === null) { localStorage.removeItem(k); } else { localStorage.setItem(k, v); } } catch (e) { /* no storage */ } }
	function stored(k) { try { return localStorage.getItem(k); } catch (e) { return null; } }
	// each runs fn on every element under root matching sel.
	function each(root, sel, fn) { Array.prototype.forEach.call(root.querySelectorAll(sel), fn); }
	// A card's close button, and opening or closing the card layer.
	var CLOSE = '<button class="link x mclose" aria-label="close">✕</button>';
	function openModal() { $('modal').classList.remove('hidden'); }
	function closeModal() { $('modal').classList.add('hidden'); }
	// A finished match from your seat: won, lost or draw; who you played;
	// the class that colours a result.
	function outcome(m) {
		if (!(m.winner >= 0 && m.you >= 0)) { return 'draw'; }
		return (m.players || []).some(function (p) { return p.slot === m.you && p.team === m.winner; }) ? 'won' : 'lost';
	}
	function opponents(m) {
		var ps = m.players || [], me = ps.filter(function (p) { return p.slot === m.you; })[0];
		return ps.filter(function (p) { return p.slot !== m.you && !(me && p.team === me.team); }).map(function (p) { return p.name; }).join(', ') || 'opponent';
	}
	function resClass(res) { return res === 'won' ? 'good' : res === 'lost' ? 'bad' : ''; }
	function heroName(id) { return (C.Heroes[id] || {}).Name || id || ''; }
	function saveGame() {
		if (!S || S.online || S.replay || S.hotseat) { return; } // an online match lives on the server
		try { keep(SAVE, G.save()); } catch (e) { /* engine gone; nothing to keep */ }
		keep(ORDERS, JSON.stringify({ turn: S.view.Match.Turn, orders: S.orders }));
	}
	function forget() { keep(SAVE, null); keep(ORDERS, null); }

	// ---- preferences: built to be looked at for thousands of games, so the
	// player picks how fast turns play back, the board's colours, and whether
	// anything moves on its own.
	var PREFS = 'rfog.prefs';
	var PREF = { speed: 'normal', board: 'fibre', motion: 'on' };
	// The boards: light squares, dark squares, walls. The terminal client
	// has the same list (render.Boards in Go; web/boards_test.go keeps the
	// two equal), so a board looks the same wherever it is played.
	var BOARDS = [
		['fibre', '#52778a', '#3d5a6b', '#0d161c'],
		['graphite', '#6e6e6c', '#555553', '#1a1a19'],
		['daylight', '#9aa3ab', '#6c757d', '#22262a'],
		['abyss', '#5d6976', '#4a5561', '#0b0d10'],
		['nord', '#81a1c1', '#5e81ac', '#2e3440'],
		['gruvbox', '#a89984', '#7c6f64', '#282828'],
		['catppuccin', '#9399b2', '#6c7086', '#11111b'],
		['tokyo-night', '#737aa2', '#565f89', '#16161e'],
		['amber', '#b08a3a', '#7d5f22', '#1f1606'],
		['green', '#3f8a46', '#2f6b35', '#0a1f0c']
	];
	function board(name) { return BOARDS.filter(function (b) { return b[0] === name; })[0] || BOARDS[0]; }
	var SPEEDS = { fast: 0.55, normal: 1, slow: 1.6, instant: 0 };
	try { var pp = JSON.parse(stored(PREFS) || '{}'); for (var k in PREF) { if (pp[k]) { PREF[k] = pp[k]; } } } catch (e) { /* defaults */ }
	// PACE squeezes a long turn while it plays (see paceFor); 1 otherwise.
	var PACE = 1;
	function K() { return (SPEEDS[PREF.speed] === undefined ? 1 : SPEEDS[PREF.speed]) * PACE; }
	function applyPrefs() {
		var root = document.documentElement;
		var b = board(PREF.board);
		PREF.board = b[0];
		root.dataset.board = b[0];
		root.style.setProperty('--light', b[1]);
		root.style.setProperty('--dark', b[2]);
		root.style.setProperty('--wall', b[3]);
		root.dataset.motion = PREF.motion;
		keep(PREFS, JSON.stringify(PREF));
		if (ART) { backdrops(); }
	}
	applyPrefs();
	// resume brings back a saved match, or says why it could not.
	function resume() {
		var sv = stored(SAVE);
		if (!sv) { return false; }
		try {
			call('load', sv);
		} catch (e) {
			forget();
			note(/rules have been updated/.test(e.message) ? 'The game was updated; your last match ended.' : '');
			return false;
		}
		try { pick.level = JSON.parse(sv).level || pick.level; } catch (e) { /* keep default */ }
		begin();
		try {
			var o = JSON.parse(stored(ORDERS) || 'null');
			if (o && o.turn === S.view.Match.Turn && o.orders && o.orders.length &&
					!(call('check', JSON.stringify(o.orders)) || []).length) {
				S.orders = o.orders;
			}
		} catch (e) { /* start the turn clean */ }
		render();
		toast('match resumed');
		return true;
	}
	function note(msg) { $('picknote').textContent = msg || ''; }

	// Anything that goes wrong is shown, and never leaves the board locked.
	window.addEventListener('error', function (ev) { oops(ev.message); });
	window.addEventListener('unhandledrejection', function (ev) { oops(ev.reason && ev.reason.message || ev.reason); });
	function oops(msg) {
		if (S) { S.busy = false; }
		try { toast('something went wrong: ' + msg); } catch (e) { /* nothing to show it on */ }
	}

	// ---- boot -----------------------------------------------------------
	window.onRfogReady = function () {
		G = window.rfogGame;
		C = call('content');
		ART = call('cast');
		backdrops();
		if (!resume()) { showPick(); }
		// An invite link (?c=CODE): sign in (a guest is fine), then show it.
		var inv = /[?&]c=([A-Za-z0-9]+)/.exec(location.search);
		if (inv) {
			NET.invite = inv[1].toUpperCase();
			try { history.replaceState(null, '', location.pathname + location.search.replace(/[?&]c=[A-Za-z0-9]+/, '').replace(/^&/, '?')); } catch (e) { /* keep the URL */ }
		}
		// Connect on open, as a chess site does: with this device's sign-in
		// if it has one (a match in progress or the last result then shows
		// at the top), else as an anonymous guest who can play casual games
		// and watch at once. Signing in is a button, never a gate.
		reconnect();
	};
	var go = new Go();
	var wasm = fetch('engine.wasm');
	(WebAssembly.instantiateStreaming ? WebAssembly.instantiateStreaming(wasm, go.importObject)
		: wasm.then(function (r) { return r.arrayBuffer(); }).then(function (b) { return WebAssembly.instantiate(b, go.importObject); }))
		.then(function (res) { go.run(res.instance); })
		.catch(function (e) {
			$('loading').innerHTML = '<h1 class="wordmark">RFoG</h1><p class="tag">rf over glass</p><small>could not start the engine: ' + e + '</small>' +
				'<button class="primary" onclick="location.reload()">Retry</button>';
		});

	function show(id) {
		$('sheet').classList.add('hidden');
		setTimeout(function () { try { banner(); } catch (e) { /* before boot */ } }, 0);
		['loading', 'pick', 'draft', 'game', 'over'].forEach(function (s) { $(s).classList.toggle('hidden', s !== id); });
	}

	// ---- figures: drawn from their pixel grids onto canvases. A canvas is
	// painted the moment it is made, so a figure never blinks while an image
	// decodes, and the team outline is drawn in rather than being a stack of
	// CSS filters the browser repaints.
	var FIG = {};
	function figure(id, team, frame, px, outline) {
		var rows = frame === 'attack' ? ((ART.poses || {})[id] || {}).attack : ART.units[id];
		if (!rows) { return null; }
		var dpr = Math.min(3, window.devicePixelRatio || 1), dp = Math.round(px * dpr);
		var o = outline ? Math.max(1, Math.round(dpr)) : 0;
		var key = [id, team, frame, dp, o].join('|');
		if (FIG[key]) { return FIG[key]; }
		// The breath: everything above the waist sinks a pixel. The waist is
		// just over half way down the figure's own height.
		var n = ART.size, top = n, bot = 0;
		rows.forEach(function (r, y) { if (/[^.]/.test(r)) { top = Math.min(top, y); bot = Math.max(bot, y); } });
		var waist = top + Math.floor((bot - top) * 0.55);
		var at = function (v) { return Math.round(v * dp / n); };
		var cv = document.createElement('canvas');
		cv.width = cv.height = dp + 2 * o;
		var g = cv.getContext('2d');
		var draw = function (ox, oy, flat, lit) {
			rows.forEach(function (row, y) {
				var yy = frame === 'b' && y < waist ? y + 1 : y;
				for (var x = 0; x < row.length; x++) {
					var c = ART.palette[row[x]];
					if (lit && c !== 'team') { continue; }
					if (c === 'team') { c = TEAM[team][0]; } else if (c === 'team-shade') { c = TEAM[team][1]; }
					if (!c) { continue; }
					g.fillStyle = flat || c;
					g.fillRect(ox + at(x), oy + at(yy), at(x + 1) - at(x), at(yy + 1) - at(yy));
				}
			});
		};
		if (o) {
			for (var dy = -1; dy <= 1; dy++) {
				for (var dx = -1; dx <= 1; dx++) { if (dx || dy) { draw(o + dx * o, o + dy * o, TEAM[team][2] || TEAM[team][0]); } }
			}
		}
		draw(o, o);
		// The eyes and lights glow in the team's colour, and flare in the
		// attack pose. Painted once here, so the glow costs nothing to show.
		g.shadowColor = TEAM[team][0];
		g.shadowBlur = dp / n * (frame === 'attack' ? 3 : 1.6);
		draw(o, o, null, true);
		if (frame === 'attack') { draw(o, o, null, true); }
		g.shadowBlur = 0;
		cv.pad = o;
		cv.dpr = dpr;
		FIG[key] = cv;
		return cv;
	}
	// paintFigure adds a canvas holding a figure to parent, its box px square
	// (an outline spills over the edge of the box).
	function paintFigure(parent, id, team, frame, px, outline, cls) {
		var f = figure(id, team, frame, px, outline);
		if (!f) { return null; }
		var c = document.createElement('canvas');
		c.width = f.width;
		c.height = f.height;
		c.getContext('2d').drawImage(f, 0, 0);
		c.className = cls;
		c.style.width = c.style.height = (f.width / f.dpr) + 'px';
		c.style.left = c.style.top = (-f.pad / f.dpr) + 'px';
		parent.appendChild(c);
		return c;
	}
	// pose shows or hides a figure's attack frame; false if it has none.
	function pose(sp, on) {
		if (!on) { sp.classList.remove('posing'); return true; }
		if (!sp.querySelector('.pose') &&
				!paintFigure(sp, sp.dataset.figure, +sp.dataset.team, 'attack', +sp.dataset.px, sp.dataset.outline === '1', 'pose')) { return false; }
		sp.classList.add('posing');
		return true;
	}
	// sprite paints a character into el, size px square. opts.breath adds the
	// second idle frame, opts.outline the team's outline.
	function sprite(el, id, size, team, opts) {
		opts = opts || {};
		each(el, 'canvas', function (c) { c.remove(); });
		if (!paintFigure(el, id, team || 0, 'a', size, opts.outline, 'f0')) { return; }
		if (opts.breath) { paintFigure(el, id, team || 0, 'b', size, opts.outline, 'breath'); }
		el.classList.add('sprite');
		el.style.width = el.style.height = size + 'px';
		el.dataset.figure = id;
		el.dataset.team = team || 0;
		el.dataset.px = size;
		el.dataset.outline = opts.outline ? '1' : '0';
	}
	function spriteID(u) { return (u.IsCommander ? 'hero_' : 'unit_') + u.Kind; }

	// ---- dither: light and shade in ordered steps (a 4x4 Bayer matrix), as
	// pixel art has it, in the board's own colours. The terminal draws the
	// same fog steps with ░▒▓: a quarter, a half, three quarters of the cells.
	var BAYER = [0, 8, 2, 10, 12, 4, 14, 6, 3, 11, 1, 9, 15, 7, 13, 5];
	var TILES = {};
	// pattern is a fill for context g: color on the cells of a 4x4 tile whose
	// Bayer rank is under level (0 none, 16 all), cell px a cell. The tile is
	// built once per colour, level and cell; the fill once per context.
	function pattern(g, color, level, cell) {
		var key = color + '|' + level + '|' + cell;
		g.fills = g.fills || {};
		if (g.fills[key]) { return g.fills[key]; }
		var t = TILES[key];
		if (!t) {
			t = TILES[key] = document.createElement('canvas');
			t.width = t.height = 4 * cell;
			var tg = t.getContext('2d');
			tg.fillStyle = color;
			BAYER.forEach(function (rank, i) { if (rank < level) { tg.fillRect(i % 4 * cell, (i >> 2) * cell, cell, cell); } });
		}
		return (g.fills[key] = g.createPattern(t, 'repeat'));
	}
	// pixels is a canvas of w by h cells, one canvas pixel a cell, shown cell
	// CSS px a cell with its pixels kept square (play.css: canvas.px).
	function pixels(cv, w, h, cell) {
		cv.width = w;
		cv.height = h;
		cv.style.width = w * cell + 'px';
		cv.style.height = h * cell + 'px';
		return cv.getContext('2d');
	}
	// backdrop puts a dithered vertical gradient behind el (w by h CSS px,
	// cells of cell px): colour a at the top, stepping to b from f0 to f1
	// of the height (past 1, b is never reached). One small canvas, drawn
	// again only when the colours or the size change.
	function backdrop(el, w, h, cell, a, b, f0, f1) {
		var cv = el.dith || (el.dith = document.createElement('canvas'));
		if (cv.parentNode !== el) { cv.className = 'px dith'; el.insertBefore(cv, el.firstChild); }
		var cw = Math.ceil(w / cell), ch = Math.ceil(h / cell), key = [a, b, cw, ch, cell, f0, f1].join('|');
		if (cv.key === key) { return; }
		cv.key = key;
		var g = pixels(cv, cw, ch, cell);
		g.fillStyle = a;
		g.fillRect(0, 0, cw, ch);
		for (var y = 0; y < ch; y++) {
			var lv = Math.round(Math.max(0, Math.min(1, (y / Math.max(1, ch - 1) - f0) / (f1 - f0))) * 16);
			if (lv) { g.fillStyle = pattern(g, b, lv, 1); g.fillRect(0, y, cw, 1); }
		}
	}
	// pool is light falling off round a point in three dither steps: a canvas
	// w by h cells (frames of them stacked, each a step dimmer) in color,
	// dense at the centre (cx, cy, fractions of it), level(r) at distance r
	// (1 at the edge of the ellipse). Kept by key: drawn once.
	var POOLS = {};
	function pool(key, w, h, cell, color, level, frames, cx, cy) {
		if (POOLS[key]) { return POOLS[key]; }
		var cv = document.createElement('canvas'), n = frames || 1, g = pixels(cv, w, h * n, cell);
		for (var f = 0; f < n; f++) {
			for (var y = 0; y < h; y++) {
				for (var x = 0; x < w; x++) {
					var dx = (x + 0.5) / w - (cx || 0.5), dy = (y + 0.5) / h - (cy || 0.5);
					var lv = Math.round(level(2 * Math.sqrt(dx * dx + dy * dy)) * (1 - f / n));
					if (lv > BAYER[(y % 4) * 4 + x % 4]) { g.fillStyle = color; g.fillRect(x, f * h + y, 1, 1); }
				}
			}
		}
		cv.className = 'px';
		return (POOLS[key] = cv);
	}
	// step is a light level stepping down with distance r: dense, half, sparse, none.
	function step(r, a, b, c) { return r < a ? 12 : r < b ? 8 : r < c ? 4 : 0; }
	// backdrops are the floors behind whole screens: home, wall stepping
	// toward the dark squares down the lower half; the end card further.
	function backdrops() {
		var th = board(PREF.board), w = window.innerWidth || 375, h = window.innerHeight || 667;
		backdrop($('pick'), w, h, 4, th[3], th[2], 0.5, 1.6);
		backdrop($('over'), w, h, 5, th[3], th[2], 0.45, 1.25);
	}

	// ---- pick -----------------------------------------------------------
	var pick = { hero: null, level: 'normal', mode: '1v1', length: 'standard' };
	// The bot card's mode and length are remembered on this device.
	var BOTS = 'rfog.bots';
	try { var bm0 = JSON.parse(stored(BOTS) || '{}'); pick.mode = bm0.mode || pick.mode; pick.length = bm0.length || pick.length; } catch (e) { /* defaults */ }
	// botMatch is the mode and length chosen, as the rules offer them (a
	// remembered one the rules no longer have falls back to the standard).
	function botMatch() {
		var R = C.Rules;
		return { mode: R.Modes[pick.mode] ? pick.mode : '1v1', length: (R.Lengths || {})[pick.length] ? pick.length : 'standard' };
	}
	// matchLength is a mode's turn limit and score to win at a length, worked
	// out as the engine does (Content.MatchLength): a length scales the
	// mode's score to win, rounded half up.
	function matchLength(mode, length) {
		var R = C.Rules, win = (R.Modes[mode] || {}).WinScore || R.WinScore, l = (R.Lengths || {})[length];
		return l ? { turns: l.MaxTurns, win: Math.floor((win * l.WinScorePercent + 50) / 100) } : { turns: R.MaxTurns, win: win };
	}
	function lengthText(mode, length) { var ml = matchLength(mode, length); return ml.turns + ' turns, first to ' + ml.win; }
	// lengthOf is the length a match is played at, from its own numbers.
	function lengthOf(v) {
		return Object.keys(C.Rules.Lengths || {}).filter(function (id) {
			var ml = matchLength(v.Match.Mode, id);
			return ml.turns === v.Match.MaxTurns && ml.win === v.Match.WinScore;
		})[0] || 'standard';
	}
	// modeText says who plays a mode, and on what board.
	function modeText(mode) {
		var m = C.Rules.Modes[mode] || {}, n = m.PlayersPerTeam || 1, map = (C.Maps || {})[m.Map];
		return (n === 1 ? 'you against a bot' : (n === 2 ? 'you and a bot ally' : 'you and ' + (n - 1) + ' bot allies') + ' against ' + n + ' bots') +
			(map ? ' on a ' + map.Width + '×' + map.Height + ' board' : '');
	}
	// matchTag names a mode and length that are not the standard 1v1.
	function matchTag(mode, length) { return (mode && mode !== '1v1' ? ' · ' + mode : '') + (length && length !== 'standard' ? ' · ' + length : ''); }
	// ---- icons: small line drawings in the signal colour, never emoji ----
	var ICONS = {
		bots: '<rect x="4" y="8" width="16" height="11" rx="3"/><path d="M12 4v4"/><circle cx="12" cy="3.5" r="1"/><circle cx="9" cy="13.5" r="1.3" fill="currentColor"/><circle cx="15" cy="13.5" r="1.3" fill="currentColor"/>',
		pass: '<path d="M5 8h13l-3-3M19 16H6l3 3"/>',
		friend: '<circle cx="9" cy="8" r="3"/><path d="M3 20c0-3.3 2.7-6 6-6s6 2.7 6 6"/><circle cx="17" cy="9" r="2.5"/><path d="M16 14.3c2.9.4 5 2.7 5 5.7"/>',
		learn: '<path d="M3 5h6a3 3 0 0 1 3 3v12a2 2 0 0 0-2-2H3zM21 5h-6a3 3 0 0 0-3 3v12a2 2 0 0 1 2-2h7z"/>',
		bullet: '<path d="M3 9h4M2 12h5M3 15h4M10 8h6a4 4 0 0 1 0 8h-6z"/>',
		blitz: '<path d="M13 2 4 14h7l-1 8 9-12h-7z"/>',
		rapid: '<circle cx="12" cy="13" r="8"/><path d="M12 9v4l3 2M10 2h4M12 2v3"/>',
		'long-haul': '<path d="M7 3h10M7 21h10M8 3c0 6 8 6 8 9s-8 3-8 9M16 3c0 6-8 6-8 9s8 3 8 9"/>',
		daily: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M2 12h2M20 12h2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>'
	};
	function svgIcon(name) {
		return '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' + (ICONS[name] || '') + '</svg>';
	}
	function icon(name) { return '<span class="ico">' + svgIcon(name) + '</span>'; }

	// ---- home: a chess site's front page ---------------------------------
	// A top bar (Play · Learn · Leaderboard, sign-in or your name), a quick
	// pairing grid of time controls, bots below, and a side column with your
	// games and the leaderboard. Cards over the page for sign-in, account and
	// seeking a game.
	var NAV = 'play';
	// Time controls as a chess site shows them: seconds a turn, the rating
	// category. The server sends its list (Welcome.clocks); this is the same
	// list for before signing in. Each can be played rated or casual.
	var CLOCKS = [['10s', 'bullet'], ['15s', 'bullet'], ['20s', 'blitz'], ['30s', 'blitz'], ['45s', 'rapid'], ['60s', 'rapid'],
		['90s+', 'long-haul'], ['120s+', 'long-haul'], ['24h', 'daily']].map(function (c) { return { id: c[0], category: c[1], async: c[0] === '24h' }; });
	function clocks() { return (NET.welcome && NET.welcome.clocks && NET.welcome.clocks.length) ? NET.welcome.clocks : CLOCKS; }
	// isDaily is whether a clock plays one move a day (daily).
	function isDaily(id) { var c = clocks().filter(function (x) { return x.id === id; })[0]; return !!(c && c.async); }
	function cap(w) { return w.charAt(0).toUpperCase() + w.slice(1); }
	// clockLabel names a match's time control: "30s · blitz · casual".
	function clockLabel(id) {
		var casual = /-casual$/.test(id), base = id.replace(/-casual$/, '');
		var c = clocks().filter(function (x) { return x.id === base; })[0];
		return base + (c ? ' · ' + c.category : '') + (casual ? ' · casual' : ' · rated');
	}
	// rated is whether quick pairing seeks rated games (guests: never).
	function rated() {
		var w = NET.welcome;
		if (w && w.guest) { return false; }
		var r = stored('rfog.rated');
		return r === null ? !!w : r === '1';
	}
	function showPick() {
		show('pick');
		pick.hero = pick.hero || Object.keys(C.Heroes).sort()[0];
		each(document, '#topnav [data-nav]', function (b) { b.onclick = function () { nav(b.dataset.nav); }; });
		$('userchip').onclick = function (ev) { ev.stopPropagation(); userMenu(); };
		$('playbots').onclick = botCard;
		$('hotseat').onclick = pickHotseat;
		$('friend').onclick = function () { challengeCard(); };
		// a donate link in the top bar, once there is somewhere to send it
		if (DONATE_URL && !document.querySelector('#topnav .donatelink')) {
			document.querySelector('#topnav .navlinks').insertAdjacentHTML('beforeend', '<a class="donatelink" href="' + DONATE_URL + '" target="_blank" rel="noopener">Donate</a>');
		}
		$('howto').onclick = function () { nav('learn'); };
		renderFoot();
		each($('pick'), 'i[data-icon]', function (el) { el.innerHTML = svgIcon(el.dataset.icon); });
		nav(NAV);
	}
	function nav(v, section) {
		NAV = v;
		['home', 'watch', 'games', 'learn', 'ladder', 'prefs'].forEach(function (id) { $(id).classList.toggle('hidden', id !== (v === 'play' ? 'home' : v)); });
		if (v === 'prefs') { PREFS_AT = section || PREFS_AT; renderPrefs(); }
		each(document, '#topnav .navlinks button', function (b) { b.classList.toggle('on', b.dataset.nav === v); });
		if (v === 'learn') { renderLearn(); }
		if (v === 'ladder') { renderLadder(); }
		if (v === 'watch') { nsend('lobby', {}); renderWatch(); }
		if (v === 'games') { nsend('history', { limit: 100 }); renderGames(); }
		renderHome();
	}
	// Watch: the matches being played now; a tap watches one live (ranked
	// matches a turn behind, so watching cannot help a player).
	function renderWatch() {
		var ms = (NET.lobby || []).filter(function (m) { return !m.bots; }); // games against bots are not shown
		var h = '<h2>Watch</h2>';
		if (!NET.welcome) {
			h += '<p class="dim">' + (NET.state === 'connecting' ? 'connecting…' : 'offline') + '</p>';
		} else if (!ms.length) {
			h += '<p class="dim">No games are currently being played.</p>';
		} else {
			LADDERS.forEach(function (cat) {
				var group = ms.filter(function (m) { return category(m.time) === cat; });
				if (!group.length) { return; }
				h += '<h4>' + icon(cat) + ' ' + cap(cat) + '</h4><ul class="watchlist">' + group.map(function (m) {
					return '<li data-wm="' + m.match + '"><b>' + (m.players || []).join(' vs ') + '</b><span>' + clockLabel(m.time) + ' · ' + m.mode + '</span>' +
						'<small>' + (m.phase === 'draft' ? 'drafting' : 'turn ' + m.turn + ' · ' + m.score[0] + '–' + m.score[1]) + (m.delayed ? ' · a turn behind' : '') + '</small></li>';
				}).join('') + '</ul>';
			});
		}
		$('watch').innerHTML = h;
		each($('watch'), '[data-wm]', function (li) {
			li.onclick = function () { nsend('spectate', { match: li.dataset.wm }); };
		});
	}
	// category is a time control's rating category ("30s-casual" is blitz).
	function category(id) {
		var c = clocks().filter(function (x) { return x.id === String(id).replace(/-casual$/, ''); })[0];
		return c ? c.category : 'other';
	}
	setInterval(function () { if ($('watch') && !$('watch').classList.contains('hidden') && $('game').classList.contains('hidden')) { nsend('lobby', {}); } }, 10000);
	// onSpec shows a frame of a match being watched: the first opens the
	// board, later ones play the turn that led to them.
	function onSpec(b) {
		var R = S && S.replay && S.replay.live ? S.replay : null;
		if (!R || R.match !== b.match) {
			if (S && !S.replay) { return; } // playing: not now
			var p0 = (b.view.Players || [])[0] || { ID: 0, Team: 0 };
			var title = (b.players || []).map(function (p) { return p.name; }).join(' vs ');
			S = { view: b.view, sel: 0, orders: [], moves: [], targets: [], abil: null, abilTargets: [], busy: false, look: 0, last: {}, scars: {},
				you: p0.ID, team: p0.Team, online: null, replay: { live: true, match: b.match, i: b.turn, n: b.live, title: title, delayed: b.delayed, time: b.time } };
			closeModal();
			show('game');
			layout();
			render();
			return;
		}
		if (S.busy) { R.pend = b; return; }
		R.n = b.live;
		if (b.ended) { R.ended = b.result || 'match over'; toast('match over · ' + R.ended); }
		if (b.events && b.events.length && b.turn > R.i) {
			S.log = logLines(b.events, S.view);
			S.last = {};
			S.scars = {};
			S.busy = true;
			render();
			S.post = b.view;
			play(b.events, function () {
				S.view = b.view;
				S.busy = false;
				R.i = b.turn;
				render();
				if (R.pend) { var nb = R.pend; R.pend = null; onSpec(nb); }
			});
			return;
		}
		S.view = b.view;
		R.i = b.turn;
		render();
	}
	// askSignIn opens the sign-in card (connecting first if needed).
	function askSignIn(form) {
		NET.wantSignin = true;
		NET.err = '';
		NET.form = form || 'login';
		if (!NET.ws && NET.state !== 'connecting') { NET.state = 'signin'; }
		renderOnline();
	}
	// quickPair seeks a game in a time control, signing in first if needed.
	function quickPair(mode) {
		var w = NET.welcome;
		if (!w) {
			NET.pendingQueue = mode;
			if (!NET.ws) { reconnect(); }
			return;
		}
		NET.mode = mode;
		NET.err = '';
		NET.hideSeek = false;
		NET.joined = true;
		nsend('queue', { mode: mode, size: '1v1', hero: pick.hero, rated: rated(), clock: true });
		NET.state = 'queued';
		NET.status = { mode: mode, waiting: 0 };
		renderOnline();
	}
	function renderHome() {
		if ($('pick').classList.contains('hidden')) { return; }
		var w = NET.welcome;
		var chip = $('userchip');
		chip.textContent = w && !w.guest ? w.name : 'Sign in';
		chip.classList.toggle('signed', !!(w && !w.guest));
		$('onlinecount').textContent = w ? w.online + ' online' : '';
		// quick pairing
		var q = $('quick');
		q.innerHTML = '';
		var isRated = rated();
		clocks().forEach(function (c) {
			var r = w && (w.ratings || {})[c.category];
			var b = document.createElement('button');
			b.className = 'tile' + (NET.state === 'queued' && NET.mode === c.id ? ' seeking' : '');
			var foot = !isRated ? 'casual' : r ? Math.round(r.rating) + ' · ' + r.wins + '–' + (r.games - r.wins) : 'rated';
			b.innerHTML = '<b>' + c.id + '</b><span>' + cap(c.category) + '</span><small>' + foot + '</small>';
			b.onclick = function () { quickPair(c.id); };
			q.appendChild(b);
		});
		// rated or casual, for every clock
		var tg = $('ratedtoggle');
		tg.innerHTML = '<button data-r="1"' + (isRated ? ' class="on"' : '') + (w && w.guest ? ' class="locked" title="register to play rated"' : '') + '>Rated</button><button data-r="0"' + (!isRated ? ' class="on"' : '') + '>Casual</button>';
		each(tg, '[data-r]', function (b) {
			b.onclick = function () {
				if (b.dataset.r === '1' && (!w || w.guest)) { askSignIn('register'); return; }
				keep('rfog.rated', b.dataset.r);
				renderHome();
			};
		});
		var lob = NET.lobby ? NET.lobby.length : 0;
		$('stats').innerHTML = w ? '<b>' + w.online + '</b> player' + (w.online === 1 ? '' : 's') + ' online' + (lob ? '<br><b>' + lob + '</b> match' + (lob === 1 ? '' : 'es') + ' in play' : '') : '';
		// your games
		var yg = '<h4 class="go" data-go="games">Games</h4>';
		if (NET.resume) {
			var f = NET.resume.found;
			yg += '<div class="row2"><span><b>In progress</b> vs ' + opponents(f) + ' · ' + (f.time || '') + '</span><button id="yrejoin" class="primary small-btn">Rejoin</button></div>';
		}
		(NET.live || []).forEach(function (lm, i) {
			var left = lm.deadline ? Math.max(0, Math.round((Date.parse(lm.deadline) - Date.now()) / 3600000)) : 0;
			yg += '<div class="row2"><span><b>Daily</b> vs ' + (lm.versus || 'opponent') + ' · ' + (lm.phase === 'draft' ? 'draft' : 'turn ' + lm.turn) +
				'<br><small class="' + (lm.your_turn ? 'warn' : 'dim') + '">' + (lm.your_turn ? 'your move · ' + left + ' h left' : 'waiting on them') + '</small></span>' +
				'<button data-live="' + i + '" class="' + (lm.your_turn ? 'primary ' : '') + 'small-btn">Open</button></div>';
		});
		if (NET.last && stored('rfog.seen') !== NET.last.match) {
			var l = NET.last, res = outcome(l);
			yg += '<div class="row2"><span>Last: <b class="' + resClass(res) + '">' + res + '</b> vs ' + opponents(l) + '</span><span class="dim">' + (l.result || '') + '</span></div>';
		}
		var recent = recentGames();
		yg += gameList(recent, 5);
		if (w) {
			var rs = Object.keys(w.ratings || {});
			yg += rs.length ? '<div class="ratings">' + rs.map(function (m) { var r = w.ratings[m]; return '<span><small>' + m + '</small><b>' + Math.round(r.rating) + '</b><i>' + r.wins + '–' + (r.games - r.wins) + '</i></span>'; }).join('') + '</div>'
				: (recent.length ? '' : '<p class="dim">No games yet.</p>');
		} else if (!recent.length) {
			yg += '<p class="dim">No games yet.</p>';
		}
		$('yourgames').innerHTML = yg;
		wireGames($('yourgames'), recent);
		each($('yourgames'), '[data-live]', function (b) { b.onclick = function () { openDaily(NET.live[+b.dataset.live]); }; });
		var yr = $('yrejoin');
		if (yr) { yr.onclick = rejoinResume; }
		// leaderboard: the top three of each category; a dash where no one is
		var lp = '<h4 class="go" data-go="ladder">Leaderboard</h4>';
		LADDERS.forEach(function (cat) {
			var rows = NET.ladders[cat] || [];
			lp += '<div class="lbcat" data-lm="' + cat + '"><small>' + icon(cat) + cap(cat) + '</small><ol>';
			for (var ti = 0; ti < 3; ti++) {
				var r = rows[ti];
				lp += r ? '<li><span>' + r.name + '</span><b>' + Math.round(r.rating.rating) + '</b></li>' : '<li class="dim"><span>—</span><b></b></li>';
			}
			lp += '</ol></div>';
		});
		$('ladderpanel').innerHTML = lp;
		each($('ladderpanel'), '[data-lm]', function (el) { el.onclick = function () { LADDER_AT = el.dataset.lm; nav('ladder'); }; });
		each($('pick'), 'h4.go', function (h) { h.onclick = function () { nav(h.dataset.go); }; });
	}
	var LADDERS = ['bullet', 'blitz', 'rapid', 'long-haul', 'daily'], LADDER_AT = 'blitz';
	// recentGames is every game this player has: online (from the server)
	// and against bots on this device, newest first.
	function recentGames() {
		var out = [];
		(NET.history || []).forEach(function (m) {
			var mine0 = (m.players || []).filter(function (p) { return p.slot === m.you; })[0] || {};
			out.push({ match: m.match, you: m.you, at: Date.parse(m.ended) || 0, res: outcome(m), what: opponents(m), as: heroName(mine0.hero), how: clockLabel(m.time).replace(/ · rated$/, '') });
		});
		localGames().forEach(function (g) {
			var team = g.mode && g.mode !== '1v1';
			out.push({ local: g.at, at: g.at, res: g.res, what: team ? 'bots' : 'bot' + (g.vs ? ' ' + heroName(g.vs) : ''), as: heroName(g.hero), how: g.level + matchTag(g.mode, g.length) });
		});
		return out.sort(function (x, y) { return y.at - x.at; });
	}
	// gameList is games as a list (the first n; all when n is 0), each
	// opening its replay when it has one.
	function gameList(games, n, dated) {
		if (!games.length) { return '<p class="dim">No games yet.</p>'; }
		return '<ul class="recent">' + games.slice(0, n || games.length).map(function (g, i) {
			var can = g.match || (g.local && stored('rfog.replay.' + g.local));
			var when = dated && g.at ? new Date(g.at).toLocaleDateString() + ' · ' : '';
			return '<li' + (can ? ' class="replayable" data-ri="' + i + '" title="watch the replay"' : '') + '><b class="' + resClass(g.res) + '">' + g.res + '</b><span>vs ' + g.what + '</span><small>' + when + (g.as ? 'as ' + g.as + ' · ' : '') + g.how + (can ? ' · replay' : '') + '</small></li>';
		}).join('') + '</ul>';
	}
	function wireGames(root, games) {
		each(root, '[data-ri]', function (li) {
			li.onclick = function () {
				var g = games[+li.dataset.ri];
				if (g.local) { openReplay(stored('rfog.replay.' + g.local), 0, 'vs ' + g.what + ' · ' + g.how); return; }
				NET.replayFor = { match: g.match, you: g.you, title: 'vs ' + g.what + ' · ' + g.how };
				nsend('replay_get', { match: g.match });
			};
		});
	}
	// renderGames is the Games page: every game, with its replay.
	function renderGames() {
		var games = recentGames();
		$('games').innerHTML = '<h2>Games</h2>' + gameList(games, 0, true);
		wireGames($('games'), games);
	}
	function renderLadder() {
		var cur = LADDER_AT, rows = NET.ladders[cur];
		var h = '<h2>Leaderboard</h2><div class="seg tabs">' + LADDERS.map(function (m) { return '<button data-lm="' + m + '"' + (m === cur ? ' class="on"' : '') + '>' + icon(m) + cap(m) + '</button>'; }).join('') + '</div>';
		if (!NET.welcome) {
			h += '<p class="dim">' + (NET.state === 'connecting' ? 'connecting…' : 'offline') + '</p>';
		} else if (rows && rows.length) {
			h += '<table class="lbt"><tr><th>#</th><th>player</th><th>rating</th><th>games</th><th>won</th></tr>' +
				rows.map(function (r, i) { return '<tr><td>' + (i + 1) + '</td><td>' + r.name + '</td><td><b>' + Math.round(r.rating.rating) + '</b> <span class="dim">±' + Math.round(r.rating.rd) + '</span></td><td>' + r.rating.games + '</td><td>' + r.rating.wins + '</td></tr>'; }).join('') + '</table>';
		} else {
			h += '<p class="dim">—</p>';
		}
		$('ladder').innerHTML = h;
		each($('ladder'), '[data-lm]', function (b) {
			b.onclick = function () { LADDER_AT = b.dataset.lm; nsend('ladder', { mode: LADDER_AT }); renderLadder(); };
		});
	}
	// Learn has two parts: how a turn works, and every commander in detail.
	var LEARN_AT = 'play';
	function renderLearn() {
		var R = C.Rules;
		var tabs = '<div class="learnbar"><div class="seg tabs learntabs"><button data-lt="play"' + (LEARN_AT === 'play' ? ' class="on"' : '') + '>How to play</button><button data-lt="cmd"' + (LEARN_AT === 'cmd' ? ' class="on"' : '') + '>Commanders</button></div>' +
			'<button id="tutorial" class="primary small-btn">Start the tutorial</button></div>';
		var body;
		if (LEARN_AT === 'cmd') {
			body = '<p class="dim lead">Ten commanders, four abilities each. Q and W are available from the start, E at level ' + R.Unlock.e + ', R at level ' + R.Unlock.r + '. Commanders gain levels from kills and from scoring alongside their units. Select a figure to see its attack.</p><div class="cmdgrid">' +
				Object.keys(C.Heroes).sort().map(commanderCard).join('') + '</div>' + unitsTable();
		} else {
			body = '<div class="learncols"><div>' +
				'<h4>The turn</h4><p>Both sides issue orders in secret. The turn then resolves for both at once, in a fixed order: instant abilities, delayed abilities, movement, overwatch, attacks, scoring. Select a unit to show its moves (dots) and targets (rings, with hit odds). Tap a square or drag the piece to order it. A commander takes two orders a turn; a unit takes one. End the turn when ready.</p>' +
				'<h4>Scoring</h4><p>Objectives are the outlined zones. A side holds an objective when only its units occupy it; any enemy unit inside contests it. ' +
				(R.Scoring === 'net' ? 'At the end of each turn, the side holding more objectives scores the difference.' : 'Each held objective scores at the end of each turn.') +
				' A commander kill scores ' + R.CommanderDeathPoints + '. First to ' + R.WinScore + ' wins; otherwise the higher score after ' + R.MaxTurns + ' turns.' +
					Object.keys(R.Lengths || {}).map(function (id) { return ' A ' + (R.Lengths[id].Name || id) + ' match against bots is ' + lengthText('1v1', id) + '.'; }).join('') + '</p>' +
				'<h4>Attacks</h4><p>An attack rolls one die per point of ATK. Each die at or above the target number on the ring hits for 1 damage. The target number rises with the target\'s DEF, cover and hold.</p>' +
				'<h4>Terrain and sight</h4><p>High ground extends range. Cover and holding raise defence. Each side sees only what its units see. A red ✕ marks where a delayed ability lands: clear the square.</p>' +
				'<h4>One account</h4><p>The same engine runs here, in the terminal and over SSH. A match continues on any client, one at a time.</p>' +
				'<h4>Play in a terminal</h4><p>Over SSH, with nothing to install:</p><pre class="cmd">ssh -p 2222 yourname@' + gameHost() + '</pre>' +
				'<p>Or download the native client from the <a href="' + REPO_URL + '/releases" target="_blank" rel="noopener">releases</a> (Linux, macOS, Windows, FreeBSD), then:</p><pre class="cmd">rfog online</pre>' +
				'</div><div><h4>Your squad</h4>' + unitsTable() + '</div></div>';
		}
		$('learn').innerHTML = '<h2>Learn</h2>' + tabs + body;
		each($('learn'), '[data-lt]', function (b) { b.onclick = function () { LEARN_AT = b.dataset.lt; renderLearn(); }; });
		$('tutorial').onclick = startTutorial;
		figs($('learn'));
	}
	// figs paints every figure under root (a commander card's, a unit's),
	// breathing; a tap or a hover on its card strikes the attack pose.
	function figs(root) {
		each(root, '[data-fig]', function (el) {
			var id = el.dataset.fig, size = +el.dataset.size || 40;
			sprite(el, id, size, 0, { breath: true });
			if (((ART && ART.poses) || {})[id] && ART.poses[id].attack) {
				var card = el.closest('.ccard') || el;
				var on = function () { pose(el, true); };
				var off = function () { pose(el, false); };
				card.addEventListener('mouseenter', on);
				card.addEventListener('mouseleave', off);
				card.addEventListener('click', function () { if (el.classList.contains('posing')) { off(); } else { on(); } });
			}
		});
	}
	// commanderCard is one commander: figure, role, numbers, abilities.
	function commanderCard(id) {
		var h = C.Heroes[id], R = C.Rules;
		var stats = [['HP', h.HP], ['MV', h.MV], ['JMP', h.JMP], ['RNG', h.RNG], ['ATK', h.ATK], ['DEF', h.DEF], ['INI', h.INI], ['VIS', h.VIS]];
		var abs = ['q', 'w', 'e', 'r'].map(function (k) {
			var ab = C.Abilities[h.Abilities[k]];
			if (!ab) { return ''; }
			var lv = (R.Unlock || {})[k] || 1;
			return '<li><span class="key">' + k.toUpperCase() + '</span><div><b>' + ab.Name + '</b>' + (lv > 1 ? ' <span class="dim">· level ' + lv + '</span>' : '') +
				'<p>' + describe(ab) + '</p><small>' + abilityMeta(ab) + '</small></div></li>';
		}).join('');
		return '<div class="ccard"><div class="cfig"><div class="figwrap"><div data-fig="hero_' + id + '" data-size="96"></div></div></div>' +
			'<div class="cbody"><h3>' + h.Name + '</h3><div class="crole">' + cap(h.Class) + ' · ' + h.Role + '</div>' +
			'<div class="cstats">' + stats.map(function (x) { return '<span><small>' + x[0] + '</small><b>' + x[1] + '</b></span>'; }).join('') + '</div>' +
			'<ul class="cabs">' + abs + '</ul></div></div>';
	}
	// abilityMeta is an ability's target, range, timing and cooldown.
	function abilityMeta(ab) {
		var parts = [];
		parts.push(ab.Target === 'self' ? 'on itself' : ab.Target === 'all' ? 'all allies' + (ab.Kind ? ' (' + ab.Kind + 's)' : '') : (ab.Range === 1 ? (ab.Target === 'unit' ? 'an adjacent unit' : 'an adjacent square') : (ab.Target === 'unit' ? 'a unit' : 'a square') + (ab.Range ? ' up to ' + ab.Range + ' away' : '')));
		if (ab.Area) { parts.push((ab.Area * 2 + 1) + '×' + (ab.Area * 2 + 1) + ' area'); }
		if (ab.Delay) { parts.push(ab.Delay === 1 ? 'lands next turn' : 'lands in ' + ab.Delay + ' turns'); }
		if (ab.Cooldown) { parts.push('cooldown ' + ab.Cooldown); }
		return parts.join(' · ');
	}
	// describe says what an ability does, from its effects in the rules.
	function describe(ab) {
		var who = { enemies: 'enemies', allies: 'allies', all: 'everyone', self: 'itself' }[ab.Filter] || '';
		var out = (ab.Effects || []).map(function (e) {
			var turns = e.Turns ? ' for ' + e.Turns + ' turn' + (e.Turns === 1 ? '' : 's') : '';
			switch (e.Kind) {
				case 'damage': return e.N + ' damage' + (e.Radius ? ' to ' + (e.Filter || who || 'units') + ' around it' : '');
				case 'heal': return 'heals ' + e.N;
				case 'shield': return 'a ' + e.N + '-point shield' + turns;
				case 'push': return 'pushes back ' + e.Dist;
				case 'pull': return 'pulls ' + e.Dist + ' squares closer';
				case 'slow': return '−' + e.MV + ' movement' + turns;
				case 'root': return 'rooted' + turns;
				case 'silence': return 'can\'t cast' + turns;
				case 'taunt': return 'must attack the caster' + turns;
				case 'cloak': return 'cloaked' + turns + ' (unseen unless adjacent)';
				case 'mark': return 'marked: attacks on it roll +' + e.Dice + ' dice' + turns;
				case 'reveal': return 'reveals a ' + (e.Radius * 2 + 1) + '×' + (e.Radius * 2 + 1) + ' area' + turns + (ab.Visible === false ? ', even through fog' : '');
				case 'smoke': return 'smoke that blocks sight' + turns;
				case 'stat': return (e.Delta > 0 ? '+' : '') + e.Delta + ' ' + (e.Stat || '').toUpperCase() + turns;
				case 'teleport': return e.Who === 'target' ? 'brings the ally to its side' : 'teleports there';
				case 'spawn': var u = (C.Units[e.Unit] || {}).Name || e.Unit; return (e.Count > 1 ? e.Count + ' ' + u + 's' : 'a ' + u) + ' appear' + (e.Count > 1 ? '' : 's') + ((C.Units[e.Unit] || {}).Expires ? (e.Count > 1 ? ' (they last ' : ' (it lasts ') + C.Units[e.Unit].Expires + ' turns)' : '');
				case 'overwatch': return (e.Shots || 1) + ' overwatch shot' + ((e.Shots || 1) > 1 ? 's' : '') + (e.RNG ? ' at range ' + e.RNG : '');
				case 'approach': return 'jumps up to ' + e.Dist + ' squares to the target';
				case 'strike': return 'and attacks it';
				case 'sacrifice': return 'the unit is used up';
			}
			return e.Kind;
		}).join(', ');
		return out.charAt(0).toUpperCase() + out.slice(1) + '.';
	}
	// unitsTable is the squad every commander brings, and the summons.
	function unitsTable() {
		var ids = ['lineman', 'ranged', 'runner', 'medic', 'junkbot', 'drone'].filter(function (id) { return C.Units[id]; });
		return '<table class="utab"><tr><th></th><th>unit</th><th>HP</th><th>MV</th><th>RNG</th><th>ATK</th><th>DEF</th><th>INI</th></tr>' + ids.map(function (id) {
			var u = C.Units[id];
			return '<tr><td><div class="ufig" data-fig="unit_' + id + '" data-size="32"></div></td><td>' + u.Name + (u.Expires ? ' <span class="dim">summon</span>' : '') + '</td><td>' + u.HP + '</td><td>' + u.MV + '</td><td>' + u.RNG + '</td><td>' + u.ATK + '</td><td>' + u.DEF + '</td><td>' + u.INI + '</td></tr>';
		}).join('') + '</table>';
	}
	// userMenu drops down from your name, as a chess site's does: account,
	// preferences, signing in or out, and the connection's ping.
	function userMenu() {
		var m = $('usermenu');
		if (!m.classList.contains('hidden')) { m.classList.add('hidden'); return; }
		var w = NET.welcome, h = '';
		if (w && !w.guest) {
			h += '<div class="um-who"><i class="dot own"></i>' + w.name + '</div><button data-um="account">Account</button>';
		} else {
			h += '<button data-um="signin">Sign in</button><button data-um="register">Register</button>';
		}
		h += '<button data-um="games">Games</button><button data-um="prefs">Preferences</button>';
		if (w && !w.guest) { h += '<button data-um="signout">Sign out</button>'; }
		if (NET.ws && NET.ping) {
			var bars = NET.ping < 80 ? 5 : NET.ping < 150 ? 4 : NET.ping < 300 ? 3 : NET.ping < 600 ? 2 : 1;
			var bh = '';
			for (var i = 0; i < 5; i++) { bh += '<i class="' + (i < bars ? 'on' : '') + '" style="height:' + (30 + 14 * i) + '%"></i>'; }
			h += '<div class="um-ping"><span>PING <b>' + NET.ping + '</b> ms</span><span class="bars own">' + bh + '</span></div>';
		} else {
			h += '<div class="um-ping dim">' + (NET.ws ? 'connected' : 'offline') + '</div>';
		}
		m.innerHTML = h;
		m.classList.remove('hidden');
		each(m, '[data-um]', function (b) {
			b.onclick = function () {
				m.classList.add('hidden');
				switch (b.dataset.um) {
					case 'account': if ($('pick').classList.contains('hidden')) { showPick(); } nav('prefs', 'account'); break;
					case 'signin': askSignIn('login'); break;
					case 'register': askSignIn('register'); break;
					case 'prefs': if ($('pick').classList.contains('hidden')) { showPick(); } nav('prefs', 'display'); break;
					case 'games': if ($('pick').classList.contains('hidden')) { showPick(); } nav('games'); break;
					case 'signout': nsend('account', { action: 'logout', token: stored(TOKEN) || '' }); break;
				}
			};
		});
	}
	document.addEventListener('click', function () { var m = $('usermenu'); if (m) { m.classList.add('hidden'); } });
	// The ping: a round trip every 10 s while connected.
	setInterval(function () {
		if (NET.ws && NET.ws.readyState === 1) { NET.pingAt = Date.now(); nsend('ping', {}); }
	}, 10000);

	// ---- challenge a friend ---------------------------------------------
	// A private match by link: open a challenge, send the link (or read the
	// code out), and the match starts when your friend takes it.
	function challengeCard() {
		var w = NET.welcome;
		if (!w) { NET.pendingChallenge = true; askSignIn(); return; }
		var mc = $('modalcard'), sel = NET.chClock || '30s';
		var h = CLOSE + '<h3>Challenge a friend</h3><div class="chips">';
		clocks().forEach(function (c) {
			if (c.async) { return; }
			h += '<button data-ck="' + c.id + '"' + (c.id === sel ? ' class="on"' : '') + '>' + c.id + '<small>' + c.category + '</small></button>';
		});
		h += '</div><button id="chgo" class="primary">Create link</button><p class="small">casual: games between friends do not change ratings. Your friend opens the link, or types the code in the terminal (online, f).</p>';
		mc.innerHTML = h;
		each(mc, '[data-ck]', function (b) { b.onclick = function () { NET.chClock = b.dataset.ck; challengeCard(); }; });
		mc.querySelector('#chgo').onclick = function () { NET.joined = true; NET.mode = null; nsend('challenge', { action: 'create', mode: sel }); };
		mc.querySelector('.mclose').onclick = function () { closeModal(); };
		openModal();
	}
	// inviteLink is the link that opens this page on a challenge.
	function inviteLink(code) {
		var q = /[?&]server=([^&]+)/.exec(location.search);
		return location.origin + location.pathname + '?c=' + code + (q ? '&server=' + q[1] : '');
	}
	function onChallenge(b) {
		var mc = $('modalcard'), w = NET.welcome || {};
		var close = function () { closeModal(); };
		if (b.status === 'cancelled') { close(); return; }
		if (b.status === 'gone') {
			NET.invite = null;
			mc.innerHTML = '<h3>That challenge is gone</h3><p class="sub">It was taken, cancelled, or it expired.</p><button class="primary" id="chok">OK</button>';
			mc.querySelector('#chok').onclick = close;
			openModal();
			return;
		}
		var label = clockLabel(b.mode + '-casual');
		if (b.from === w.name && !NET.invite) {
			// our challenge, open: the link to send
			var link = inviteLink(b.code);
			mc.innerHTML = '<h3>Challenge open</h3><p class="sub">' + label + '</p>' +
				'<code class="code">' + b.code + '</code><input class="linkbox" readonly value="' + link + '">' +
				'<div class="levels two"><button id="chcopy">Copy link</button><button id="chshare">Share</button></div>' +
				'<span class="acquire"><i></i><i></i><i></i><i></i><i></i></span><p class="sub">waiting for your friend…</p>' +
				'<button id="chcancel" class="primary ghost">Cancel</button>';
			mc.querySelector('#chcopy').onclick = function () {
				var box = mc.querySelector('.linkbox');
				box.select();
				try { navigator.clipboard.writeText(link); } catch (e) { document.execCommand('copy'); }
				this.textContent = 'Copied';
			};
			mc.querySelector('#chshare').onclick = function () {
				if (navigator.share) { navigator.share({ title: 'RFoG', text: 'Play me in RFoG (' + label + ')', url: link }).catch(function () {}); }
				else { mc.querySelector('#chcopy').click(); }
			};
			mc.querySelector('#chcancel').onclick = function () { NET.joined = false; nsend('challenge', { action: 'cancel' }); close(); };
			openModal();
			return;
		}
		// someone's challenge, from a link: accept or decline
		mc.innerHTML = '<h3>' + b.from + ' challenges you</h3><p class="sub">' + label + ' · 1v1</p>' +
			'<div class="levels two"><button id="chaccept" class="primary">Accept</button><button id="chdecline">Decline</button></div>';
		mc.querySelector('#chaccept').onclick = function () { NET.joined = true; NET.mode = null; nsend('challenge', { action: 'accept', code: b.code }); NET.invite = null; close(); };
		mc.querySelector('#chdecline').onclick = function () { NET.invite = null; close(); };
		openModal();
	}

	// ---- preferences: a chess site's settings page ------------------------
	// One page, sections down the side: account, display, game, language,
	// privacy, support. Account actions open the same cards as before.
	var PREFS_AT = 'account';
	// DONATE_URL, when set, shows a Support section and a donate link.
	var DONATE_URL = '';
	// REPO_URL is the source code (and the terminal client's downloads).
	var REPO_URL = 'https://github.com/rfog-org/rfog';
	// HOME_HOST is the public server, named in the SSH line wherever the
	// page is not served by a real host of its own (a local server, a
	// development tunnel); a self-hosted server names itself.
	var HOME_HOST = 'rfog.org';
	function gameHost() {
		var meta = document.querySelector('meta[name="rfog-server"]');
		var h = ((meta && meta.content) || location.hostname || '').replace(/^wss?:\/\//, '').replace(/[:/].*$/, '');
		if (!h || /^(localhost|127\.|10\.|192\.168\.|\[?::1)/.test(h) || /\.(trycloudflare\.com|ngrok\.io|ngrok-free\.app|local)$/.test(h)) { return HOME_HOST; }
		return h;
	}
	// renderFoot is the site footer under every page of the home screen.
	function renderFoot() {
		var links = [['Source code', REPO_URL], ['Terminal client', REPO_URL + '/releases'], ['Report a bug', REPO_URL + '/issues']];
		if (DONATE_URL) { links.push(['Donate', DONATE_URL]); }
		$('sitefoot').innerHTML = '<div><b>RFoG</b> <span class="dim">RF over Glass · free and open source (AGPL-3.0) · no ads, no tracking</span></div><nav>' +
			links.map(function (l) { return '<a href="' + l[1] + '" target="_blank" rel="noopener">' + l[0] + '</a>'; }).join('') + '</nav>';
	}
	function renderPrefs() {
		var w = NET.welcome;
		var secs = [['account', 'Account'], ['display', 'Display'], ['game', 'Game'], ['language', 'Language'], ['privacy', 'Privacy']];
		if (DONATE_URL) { secs.push(['support', 'Support RFoG']); }
		var h = '<div class="prefs"><nav class="prefnav">' + secs.map(function (x) { return '<button data-ps="' + x[0] + '"' + (x[0] === PREFS_AT ? ' class="on"' : '') + '>' + x[1] + '</button>'; }).join('') + '</nav><section class="prefbody">';
		var row = function (label, ctl, hint) { return '<div class="prow"><div><b>' + label + '</b>' + (hint ? '<small>' + hint + '</small>' : '') + '</div><div>' + ctl + '</div></div>'; };
		var seg = function (key, vals, cur) { return '<div class="seg">' + vals.map(function (v) { return '<button data-pk="' + key + '" data-pv="' + v[0] + '"' + (v[0] === cur ? ' class="on"' : '') + '>' + v[1] + '</button>'; }).join('') + '</div>'; };
		switch (PREFS_AT) {
			case 'account':
				h += '<h2>Account</h2>';
				if (!w) {
					h += '<p class="dim">Not signed in. Play as a guest with just a name, or make an account (a name and a password) to keep ratings and history on every device.</p><button class="primary" data-pa="signin">Sign in</button>';
				} else if (w.guest) {
					h += row('Playing as', '<b>' + w.name + '</b> <span class="dim">guest</span>') +
						row('Save my progress', '<button class="primary small-btn" data-pa="save">Make an account</button>', 'keeps everything you have played; works on every device') +
						row('Sign out', '<button data-pa="logout">Sign out</button>');
				} else {
					h += row('Name', '<b>' + w.name + '</b>') +
						row('Password', '<button data-pa="password">Change password</button>') +
						row('Recovery code', '<button data-pa="recovery">New recovery code</button>', 'the way back if you forget your password; there is no email') +
						row('Devices', '<button data-pa="logout">Sign out here</button> <button data-pa="logout_all">Sign out everywhere</button>') +
						row('Delete account', '<button class="danger" data-pa="delete">Delete…</button>', 'removes your account, ratings and history');
					var rs = Object.keys(w.ratings || {});
					if (rs.length) {
						h += '<h4>Ratings</h4><div class="ratings">' + rs.map(function (m) { var r = w.ratings[m]; return '<span><small>' + m + '</small><b>' + Math.round(r.rating) + '</b><i>' + r.wins + '–' + (r.games - r.wins) + '</i></span>'; }).join('') + '</div>' +
							'<p class="small dim">Glicko-2, the system Lichess uses. RFoG is in alpha: ratings may be reset.</p>';
					}
				}
				break;
			case 'display':
				h += '<h2>Display</h2>' +
					row('Board', '<div class="swatches">' + BOARDS.map(function (b) {
						return '<button data-pk="board" data-pv="' + b[0] + '" class="sw' + (PREF.board === b[0] ? ' on' : '') + '" title="' + b[0] + '"><i style="background:linear-gradient(135deg,' + b[1] + ' 50%,' + b[2] + ' 50%)"></i><small>' + b[0] + '</small></button>'; }).join('') + '</div>') +
					row('Ambient motion', seg('motion', [['on', 'On'], ['off', 'Off']], PREF.motion), 'idle animations, signal pulses, ripples');
				break;
			case 'game':
				h += '<h2>Game</h2>' +
					row('Turn playback', seg('speed', [['slow', 'Slow'], ['normal', 'Normal'], ['fast', 'Fast'], ['instant', 'Instant']], PREF.speed), 'tap the board during playback to skip') +
					row('Quick pairing', seg('rated', [['1', 'Rated'], ['0', 'Casual']], rated() ? '1' : '0'), w && w.guest ? 'guests play casual' : 'the default for the grid on Play');
				break;
			case 'language':
				h += '<h2>Language</h2>' + row('Language', seg('lang', [['en', 'English']], 'en'), 'English only for now; more languages to come');
				break;
			case 'privacy':
				h += '<h2>Privacy</h2><p class="dim">An account is a name and a password. The server keeps your name, your password and recovery code as one-way hashes (argon2id: nobody can read them back), your ratings, and your match history and replays. No email, no real name, no tracking, no analytics.</p>' +
					'<p class="dim">Games against bots run on your device and stay there. Deleting your account removes it all; matches you played stay in your opponents\' history as "(deleted)".</p>';
				break;
			case 'support':
				h += '<h2>Support RFoG</h2><p class="dim">RFoG is free, with no ads and nothing to buy that changes a match. If you enjoy it, you can help keep the servers running.</p><a class="primary donate" href="' + DONATE_URL + '" target="_blank" rel="noopener">Donate</a>';
				break;
		}
		$('prefs').innerHTML = h + '</section></div>';
		each($('prefs'), '[data-ps]', function (b) { b.onclick = function () { PREFS_AT = b.dataset.ps; renderPrefs(); }; });
		each($('prefs'), '[data-pk]', function (b) {
			b.onclick = function () {
				var k = b.dataset.pk, v = b.dataset.pv;
				if (k === 'rated') { if (v === '1' && (!w || w.guest)) { return; } keep('rfog.rated', v); }
				else if (k !== 'lang') { PREF[k] = v; applyPrefs(); }
				renderPrefs();
			};
		});
		each($('prefs'), '[data-pa]', function (b) {
			b.onclick = function () {
				var a = b.dataset.pa;
				NET.err = '';
				if (a === 'signin') { askSignIn(); return; }
				if (a === 'logout' || a === 'logout_all') { nsend('account', { action: a, token: stored(TOKEN) || '' }); return; }
				NET.acct = { form: a, sure: false };
				renderOnline();
			};
		});
	}

	// ---- replays ---------------------------------------------------------
	// A finished match, turn by turn, with the whole board shown: from the
	// server (online matches) or this device (bot games).
	function openReplay(json, you, title) {
		var info;
		try { info = call('replayLoad', json); } catch (e) { note('cannot open that replay: ' + e.message); return; }
		if (!info.turns) { note('that replay has no turns'); return; }
		var me0 = (info.start.Players || []).filter(function (p) { return p.ID === you; })[0] || { Team: 0 };
		S = { view: info.start, sel: 0, orders: [], moves: [], targets: [], abil: null, abilTargets: [], busy: false, look: 0, last: {}, scars: {},
			you: you, team: me0.Team, online: null, replay: { i: 0, n: info.turns, playing: false, title: title || 'replay', start: info.start } };
		closeModal();
		show('game');
		layout();
		render();
	}
	function closeReplay() {
		var R = S && S.replay;
		clearTimeout(R ? R.timer : 0);
		if (R && R.live) { nsend('unspectate', { match: R.match }); NAV = 'watch'; }
		S = null;
		showPick();
	}
	// replayStep moves to turn i: forward one plays the turn, else it jumps.
	function replayStep(i) {
		var R = S.replay;
		i = Math.max(0, Math.min(R.n, i));
		if (S.busy) { return; }
		S.last = {};
		S.scars = {};
		if (i === R.i + 1) {
			var t = call('replayTurn', R.i);
			S.view = t.pre;
			S.log = logLines(t.events || [], t.pre);
			(t.events || []).forEach(function (e) {
				if (e.k !== 'Moved') { return; }
				var to = e.path && e.path.length ? e.path[e.path.length - 1] : e.to;
				S.last[e.from.X + ',' + e.from.Y] = true;
				S.last[to.X + ',' + to.Y] = true;
			});
			S.busy = true;
			render();
			S.post = t.post;
			play(t.events || [], function () {
				S.view = t.post;
				S.busy = false;
				R.i = i;
				render();
				if (R.playing && R.i < R.n) { R.timer = setTimeout(function () { replayStep(R.i + 1); }, 500 * Math.max(K(), 0.3)); } else { R.playing = false; render(); }
			});
			return;
		}
		R.i = i;
		S.view = i === 0 ? R.start : call('replayTurn', i - 1).post;
		render();
	}
	function replayControls(nav) {
		var R = S.replay;
		nav.classList.add(R.live ? 'one' : 'six');
		var btn = function (label, sub, on, cls, off) {
			var b = document.createElement('button');
			b.innerHTML = label + (sub ? '<small>' + sub + '</small>' : '');
			if (cls) { b.className = cls; }
			b.disabled = !!off;
			b.onclick = on;
			nav.appendChild(b);
		};
		btn('Start', '', function () { R.playing = false; replayStep(0); }, '', R.i === 0 || S.busy);
		btn('Back', '', function () { R.playing = false; replayStep(R.i - 1); }, '', R.i === 0 || S.busy);
		btn(R.playing ? 'Pause' : 'Play', 'turn ' + R.i + '/' + R.n, function () {
			R.playing = !R.playing;
			if (R.playing && R.i < R.n) { replayStep(R.i + 1); } else { clearTimeout(R.timer); render(); }
		}, 'go', R.i >= R.n && !R.playing);
		btn('Next', '', function () { R.playing = false; replayStep(R.i + 1); }, '', R.i >= R.n || S.busy);
		btn('End', '', function () { R.playing = false; replayStep(R.n); }, '', R.i >= R.n || S.busy);
		btn('Close', '', closeReplay, '', false);
	}

	// botCard sets up a match against bots on this device: bot level, length
	// and mode, each a row of choices from the rules, and what the choice
	// means. Play goes on to the pick screen.
	function botCard() {
		var mc = $('modalcard'), R = C.Rules, bm = botMatch();
		var by = function (n) { return function (a, b) { return n(a) - n(b) || (a < b ? -1 : 1); }; };
		var modes = Object.keys(R.Modes).sort(by(function (m) { return R.Modes[m].PlayersPerTeam; }));
		var lengths = ['standard'].concat(Object.keys(R.Lengths || {}).sort(by(function (l) { return R.Lengths[l].MaxTurns; })));
		var row = function (key, vals, cur, label) {
			if (vals.length < 2) { return ''; }
			return '<div class="levels" style="grid-template-columns:repeat(' + vals.length + ',1fr)">' + vals.map(function (v) {
				return '<button data-' + key + '="' + v + '"' + (v === cur ? ' class="on"' : '') + '>' + label(v) + '</button>'; }).join('') + '</div>';
		};
		var team = R.Modes[bm.mode].PlayersPerTeam > 1;
		mc.innerHTML = CLOSE + '<h3>Play against bots</h3>' +
			row('lv', ['easy', 'normal', 'hard'], pick.level, cap) +
			row('len', lengths, bm.length, function (l) { return cap(l === 'standard' ? l : R.Lengths[l].Name || l); }) +
			row('md', modes, bm.mode, function (m) { return m; }) +
			'<p class="small"><b>' + lengthText(bm.mode, bm.length) + '</b> · ' + modeText(bm.mode) +
			(team ? '<br>team modes are experimental: unbalanced, and likely to change' : '') + '</p>' +
			'<button id="bstart" class="primary">Play</button>';
		var th = board(PREF.board);
		backdrop(mc, mc.offsetWidth, mc.offsetHeight, 4, th[3], th[2], 0.4, 1.6);
		each(mc, '[data-lv]', function (b) { b.onclick = function () { pick.level = b.dataset.lv; botCard(); }; });
		var choose = function (k, v) { pick[k] = v; keep(BOTS, JSON.stringify({ mode: pick.mode, length: pick.length })); botCard(); };
		each(mc, '[data-len]', function (b) { b.onclick = function () { choose('length', b.dataset.len); }; });
		each(mc, '[data-md]', function (b) { b.onclick = function () { choose('mode', b.dataset.md); }; });
		mc.querySelector('#bstart').onclick = function () { closeModal(); pickBots(botMatch()); };
		mc.querySelector('.mclose').onclick = function () { closeModal(); };
		openModal();
	}

	// ---- the pick screen ------------------------------------------------
	// Every seat along the top, your team on the left in your colour and
	// theirs on the right, every commander below as a figure on the board's
	// own floor. A tap shows one in full; Lock in takes it. Against bots the
	// bots then fill their seats one by one; pass and play picks twice; an
	// online draft fills it from the server as each seat bans and picks.
	var PK = null;
	// openPicks shows the pick screen for o: seats [{name, team (0 left, 1
	// right), me (picks on this device), hero}], turn (the seat choosing,
	// -1 for none), sel (the commander in hand), tag (what the match is),
	// lock(id) for Lock in, back (offline, before the picks are in), and for
	// a draft heroes, banned and kind (ban or pick).
	function openPicks(o) {
		PK = o;
		clearInterval(clockTimer);
		$('draftclock').textContent = '';
		$('pktag').textContent = o.tag || '';
		var wide = Math.min(window.innerWidth || 375, 640), per = Math.max(1, o.seats.length / 2);
		// slots as big as a side's share of the floor allows, in whole pixels of the figure
		var spx = ART.size * Math.max(1, Math.min(3, Math.floor((wide - 48) / (2 * per) / (ART.size + 4))));
		var bar = $('draftseats');
		bar.textContent = '';
		bar.classList.toggle('many', per > 2);
		[0, 1].forEach(function (t) {
			var half = document.createElement('div');
			half.className = 'side ' + (t ? 'enemy' : 'own');
			o.seats.forEach(function (seat) {
				if (seat.team !== t) { return; }
				seat.el = document.createElement('div');
				seat.el.className = 'slot';
				seat.el.innerHTML = '<div class="well" style="width:' + spx + 'px;height:' + spx + 'px"></div><small></small><b></b>';
				seat.el.querySelector('small').textContent = seat.name;
				seat.px = spx;
				half.appendChild(seat.el);
			});
			bar.appendChild(half);
			if (!t) { bar.insertAdjacentHTML('beforeend', '<i class="vs">vs</i>'); }
		});
		var grid = $('draftheroes'), gpx = ART.size * (wide < 560 ? 2 : 3);
		grid.textContent = '';
		(o.heroes || Object.keys(C.Heroes).sort()).forEach(function (id, i) {
			var h = C.Heroes[id] || { Name: id, Role: '' };
			var b = document.createElement('button'), fl = document.createElement('div'), sp = document.createElement('div');
			b.className = 'pk';
			b.dataset.h = id;
			fl.className = 'fl';
			fl.appendChild(sp);
			b.appendChild(fl);
			sprite(sp, 'hero_' + id, gpx, 0, { breath: true });
			sp.style.setProperty('--d', (-(i * 0.37) % 1.8) + 's');
			b.insertAdjacentHTML('beforeend', '<span class="name">' + h.Name + '</span><span class="role"></span>');
			b.onclick = function () { if (!PK.cut) { PK.sel = id; drawPicks(); } };
			grid.appendChild(b);
		});
		o.info = undefined;
		show('draft');
		$('draft').scrollTop = 0;
		// the floors, wall stepping to the light squares, in cells of two of
		// the grid's figure pixels (one size for every commander's patch)
		var th = board(PREF.board), cell = gpx / ART.size * 2, fls = grid.querySelectorAll('.fl'), fw = fls[0].clientWidth, fh = fls[0].clientHeight;
		backdrop(bar, bar.clientWidth, bar.clientHeight, cell, th[3], th[1], 0.1, 1);
		each(grid, '.fl', function (fl) { backdrop(fl, fw, fh, cell, th[3], th[1], 0, 1); });
		drawPicks();
	}
	// taker is the seat on the choosing seat's team that already has id
	// (none may take it again: the server's draft and the bots' draw both
	// refuse a repeat within a team; the two teams may share one).
	function taker(id) {
		var cur = PK.seats[PK.turn];
		return PK.seats.filter(function (s) { return s.hero === id && s !== cur && s.team === (cur ? cur.team : 0); })[0];
	}
	function canLock(id) { return !!id && !taker(id) && (PK.banned || []).indexOf(id) < 0 && !!C.Heroes[id]; }
	// drawPicks brings the pick screen up to PK: a slot that has just been
	// filled flashes and its figure settles, the commanders taken or banned
	// dim, the one in hand shows in full.
	function drawPicks() {
		var o = PK, cur = o.seats[o.turn];
		o.seats.forEach(function (seat) {
			seat.el.classList.toggle('picking', seat === cur);
			if (!seat.hero || seat.wait || seat.hero === seat.shown) { return; }
			seat.shown = seat.hero;
			var well = seat.el.querySelector('.well'), sp = document.createElement('div');
			well.textContent = '';
			well.appendChild(sp);
			sprite(sp, 'hero_' + seat.hero, seat.px, seat.team, { breath: true });
			seat.el.querySelector('b').textContent = heroName(seat.hero);
			seat.el.classList.add('filled');
			if (!still()) { seat.el.classList.add('lockin'); }
		});
		each($('draftheroes'), '.pk', function (b) {
			var id = b.dataset.h, mate = taker(id), ban = (o.banned || []).indexOf(id) >= 0;
			b.classList.toggle('on', id === o.sel);
			b.classList.toggle('taken', !!mate);
			b.classList.toggle('banned', ban);
			b.classList.toggle('foe', !mate && o.seats.some(function (s) { return s.hero === id && s.team !== (cur ? cur.team : 0); }));
			b.disabled = !!mate || ban;
			b.querySelector('.role').textContent = ban ? 'banned' : mate ? 'taken · ' + mate.name : (C.Heroes[id] || {}).Role || '';
			pose(b.querySelector('.sprite'), id === o.sel && !b.disabled);
		});
		if (o.info !== o.sel) {
			o.info = o.sel;
			$('pkinfo').innerHTML = o.sel ? commanderCard(o.sel) : '';
			figs($('pkinfo'));
		}
		var mine = cur && cur.me, name = heroName(o.sel);
		$('draftsub').textContent = o.err || (!cur ? o.note || '' : !mine ? cur.name + (o.kind === 'ban' ? ' is banning' : ' is choosing') :
			o.kind === 'ban' ? 'Ban a commander' : (o.hotseat ? cap(cur.name) + ': choose' : 'Choose your commander'));
		var lk = $('pklock');
		lk.disabled = !(mine && canLock(o.sel) && !o.sent);
		lk.textContent = !cur ? (o.note || 'Locked in') : !mine ? 'Waiting for ' + cur.name : !o.sel ? 'Choose a commander' :
			!canLock(o.sel) ? name + ' is taken' : (o.kind === 'ban' ? 'Ban ' : 'Lock in ') + name;
		$('pkback').style.visibility = o.back && cur ? '' : 'hidden';
	}
	$('pklock').onclick = function (ev) {
		ev.stopPropagation(); // the tap that locks in does not also cut the reveal it starts
		if (PK && !this.disabled && PK.seats[PK.turn] && canLock(PK.sel)) { PK.err = ''; PK.lock(PK.sel); }
	};
	$('pkback').onclick = function () { if (PK) { clearTimeout(PK.timer); } PK = null; showPick(); };
	// A tap anywhere while the seats fill in fills them all at once.
	$('draft').addEventListener('click', function () { if (PK && PK.cut) { PK.cut(); } });
	// hold shows the full lineup for a beat (a tap cuts it), then goes on.
	function hold(go) {
		var o = PK;
		o.cut = function () { clearTimeout(o.timer); o.cut = null; if (PK === o) { go(); } };
		if (still()) { o.cut(); } else { o.timer = setTimeout(o.cut, 700); }
	}
	// snake is the order the seats choose in, as the server's draft has
	// it: the first seat, then the teams by turns (A B B A A B ...).
	function snake(per) {
		var out = [], next = [0, per];
		for (var i = 0; i < 2 * per; i++) {
			var t = i % 4 === 1 || i % 4 === 2 ? 1 : 0;
			if (next[t] >= (t + 1) * per) { t = 1 - t; }
			out.push(next[t]++);
		}
		return out;
	}
	// pickBots is the pick screen for a match against bots (o: its mode and
	// length). Lock in starts the match in the engine, which draws the bots'
	// commanders; their seats then fill one by one and the board opens.
	// again (Play again): your commander is already in, straight to that.
	function pickBots(o, again) {
		var per = C.Rules.Modes[o.mode].PlayersPerTeam, seats = [];
		for (var i = 0; i < 2 * per; i++) { seats.push({ name: i ? 'bot' : 'you', team: i < per ? 0 : 1, me: !i }); }
		openPicks({ seats: seats, turn: 0, sel: pick.hero, back: true, tag: pick.level + (per > 1 ? ' bots · ' : ' bot · ') + o.mode + (per > 1 ? ' (experimental)' : '') + ' · ' + lengthText(o.mode, o.length),
			lock: function (id) {
				var pk = PK;
				pick.hero = id;
				try { newBotMatch(o); } catch (e) { pk.err = e.message; drawPicks(); return; }
				var heroes = call('gameReplay').setup.Players.map(function (p) { return p.Hero; });
				var order = snake(per).slice(1);
				pk.turn = -1;
				pk.note = per > 1 ? 'The bots are choosing' : 'The bot is choosing';
				pk.seats[0].hero = heroes[0];
				$('draft').scrollTop = 0;
				var fill = function () { var s = order.shift(); pk.seats[s].hero = heroes[s]; if (!order.length) { pk.note = 'Ready'; } drawPicks(); };
				var go = function () { begin(); render(); saveGame(); };
				var step = function () {
					if (PK !== pk) { return; } // left the screen
					if (!order.length) { hold(go); return; }
					fill();
					pk.timer = setTimeout(step, 420);
				};
				pk.cut = function () { clearTimeout(pk.timer); while (order.length) { fill(); } hold(go); if (pk.cut) { pk.cut(); } };
				drawPicks();
				if (still()) { pk.cut(); } else { pk.timer = setTimeout(step, 450); }
			} });
		if (again) { PK.lock(pick.hero); }
	}
	// pickHotseat is the pick screen for pass and play: player 1 picks, then
	// player 2, on the same screen.
	function pickHotseat() {
		openPicks({ seats: [{ name: 'player 1', team: 0, me: true }, { name: 'player 2', team: 1, me: true }], turn: 0, sel: pick.hero, back: true, hotseat: true,
			tag: 'pass and play · plan, hand over the device, plan; the turn plays out for both',
			lock: function (id) {
				var pk = PK;
				pk.seats[pk.turn].hero = id;
				if (pk.turn === 0) { pick.hero = id; pk.turn = 1; pk.sel = pick.hero2 || null; drawPicks(); return; }
				pick.hero2 = id;
				pk.turn = -1;
				pk.note = 'Ready';
				drawPicks();
				hold(startHotseat);
			} });
	}

	function startHotseat() {
		try { call('newHotseat', pick.hero, pick.hero2, Math.floor(Math.random() * 1e9)); } catch (e) { note(e.message); return; }
		note('');
		begin();
		S.hotseat = true;
		handOver(0, call('view'));
	}
	// handOver covers the board until the next player is at the device,
	// then shows that player's side.
	function handOver(p, view) {
		var cover = $('pass') || document.body.appendChild(Object.assign(document.createElement('div'), { id: 'pass' }));
		cover.innerHTML = '<h2>Player ' + (p + 1) + '</h2><p class="sub">your turn to plan · the other player looks away</p><button class="primary">Ready</button>';
		cover.classList.remove('hidden');
		cover.querySelector('button').onclick = function () {
			cover.classList.add('hidden');
			S.view = view;
			var pl = (view.Players || []).filter(function (x) { return x.ID === p; })[0] || { Team: p };
			S.you = p;
			S.team = pl.Team;
			S.sel = 0;
			S.look = 0;
			S.orders = [];
			layout();
			render();
		};
	}

	// ---- the tutorial ---------------------------------------------------
	// A match against an easy bot with a coach over it: five lessons, each
	// cleared by doing what it asks. It never takes control; a player who
	// ignores it is simply playing.
	var COACH = [
		{ t: 'Your squad', b: 'Your commander and four units deploy nearest you. Select one.',
			ok: function () { return S.sel && mine(unit(S.sel)); } },
		{ t: 'Movement', b: 'Dots mark the squares the unit can reach. Tap one to order a move. Climbing costs extra movement.',
			ok: function () { return S.orders.some(function (o) { return o.Action === 'move'; }); } },
		{ t: 'Objectives', b: 'At the end of each turn, the side holding more objectives scores the difference. Order your units, then end the turn.',
			ok: function () { return S.view.Match.Turn > 1; } },
		{ t: 'Resolution', b: 'Both sides planned in secret; the turn resolved at once. Rings mark enemies in range, with hit odds. Tap one to order an attack.',
			ok: function () { return S.orders.some(function (o) { return o.Action === 'attack'; }) || S.view.Match.Turn > 2; } },
		{ t: 'Abilities', b: 'Abilities lists the selected unit\'s abilities. Hold raises defence. Watch fires on the first enemy to move into range.',
			ok: function () { return S.view.Match.Turn > 3; } }
	];
	function startTutorial() {
		pick.level = 'easy';
		showPick();
		startGame({ mode: '1v1', length: 'standard' });
		if (S) { S.coach = { step: 0 }; render(); }
	}
	// renderCoach advances the lesson the player has completed and shows the next.
	function renderCoach() {
		var el = $('coach');
		if (!S || !S.coach) { el.classList.add('hidden'); return; }
		var c = S.coach;
		while (c.step < COACH.length && COACH[c.step].ok()) { c.step++; }
		if (c.step >= COACH.length) {
			el.innerHTML = '<b>Tutorial complete</b><p>Play the match out. Learn has the full rules.</p><button class="link">Close</button>';
		} else {
			var l = COACH[c.step];
			el.innerHTML = '<small>' + (c.step + 1) + ' / ' + COACH.length + '</small><b>' + l.t + '</b><p>' + l.b + '</p><button class="link">Skip</button>';
		}
		el.querySelector('button').onclick = function () { S.coach = null; renderCoach(); };
		el.classList.remove('hidden');
	}

	// ---- the match ------------------------------------------------------
	var S = null; // the screen's state: view, selection, planned orders
	// newBotMatch starts a match against bots in the engine: the bot card's
	// mode and length, or o's. Seats beside you and against you are bots;
	// the engine draws their commanders from the seed.
	function newBotMatch(o) {
		o = o && o.mode ? o : botMatch();
		var opts = { mode: o.mode, length: o.length === 'standard' ? '' : o.length, hero: pick.hero, others: [], level: pick.level, seed: Math.floor(Math.random() * 1e9) };
		call('start', JSON.stringify(opts));
	}
	// startGame starts a match against bots and opens the board at once
	// (the tutorial).
	function startGame(o) {
		try { newBotMatch(o); } catch (e) { note(e.message); return; }
		note('');
		begin();
		render();
		saveGame();
	}
	// sameMatch is the mode and length of the match on the board, for Play again.
	function sameMatch() { return { mode: S.view.Match.Mode, length: lengthOf(S.view) }; }
	// begin shows the match the engine now holds, from the first seat.
	function begin() {
		clearInterval(clockTimer);
		if ($('clock')) { $('clock').textContent = ''; }
		var v = call('view'), p0 = (v.Players || [])[0] || { ID: 0, Team: 0 };
		S = { view: v, sel: 0, orders: [], moves: [], targets: [], abil: null, abilTargets: [], busy: false, look: 0, last: {}, scars: {}, you: p0.ID, team: p0.Team, online: null };
		$('sheet').classList.add('hidden');
		show('game');
		layout();
	}

	// Orientation, as a chess board sits in front of you: your side at the
	// bottom. Team 0 deploys on the left edge (x = 0), so the board is turned
	// a quarter: screen column = board y, screen row = W-1-x.
	function W() { return S.view.Board.W; }
	function H() { return S.view.Board.H; }
	// Team 1 deploys on the far edge: its board is turned the other way, so
	// either side plays from the bottom.
	function toScreen(p) { return S.team === 1 ? { c: H() - 1 - p.Y, r: p.X } : { c: p.Y, r: W() - 1 - p.X }; }
	function toBoard(c, r) { return S.team === 1 ? { X: r, Y: H() - 1 - c } : { X: W() - 1 - r, Y: c }; }
	function cols() { return H(); }
	function rows() { return W(); }
	// The board's width is measured once a render (and again after a
	// resize): asking the page for it between writes makes it lay out anew
	// each time, and a turn playing out asks hundreds of times.
	var BW = 0;
	// Under the camera a square is as wide as the room shows whole columns
	// of at least CAM.floor px (all of them if they fit), so a 2v2 fills a
	// phone's width and a 5v5 shows most of its columns.
	function sqSize() {
		if (!BW) { measure(); }
		return BW / (CAM.on ? Math.min(cols(), Math.max(1, Math.floor(BW / CAM.floor))) : cols());
	}
	// measure is the one layout read: the room's width (and height, under
	// the camera; both in one pass).
	function measure() {
		var bw = $('boardwrap');
		BW = bw.clientWidth;
		CAM.vh = CAM.on ? bw.clientHeight || BW : 0;
	}
	function center(p) {
		var s = toScreen(p), z = sqSize();
		return { x: (s.c + 0.5) * z, y: (s.r + 0.5) * z };
	}
	function tile(p) { return S.view.Board.Tiles[p.Y * W() + p.X]; }
	function same(a, b) { return a && b && a.X === b.X && a.Y === b.Y; }
	function unitAt(p) {
		var us = S.view.Units;
		for (var i = 0; i < us.length; i++) {
			if (!us[i].Dead && us[i].HP > 0 && same(us[i].Pos, p)) { return us[i]; }
		}
		return null;
	}
	function unit(id) {
		var us = S.view.Units;
		for (var i = 0; i < us.length; i++) { if (us[i].ID === id) { return us[i]; } }
		return null;
	}
	function me() { return S.view.Players.filter(function (p) { return p.ID === S.you; })[0] || S.view.Players[0]; }
	function mine(u) { return u && u.Owner === S.you; }
	// rel is a team as seen from here: 0 yours, 1 theirs (colours, sides).
	function rel(team) { return team === S.team ? 0 : 1; }
	function them() { return foes()[0] || {}; }
	// foes are the players on the other side (more than one in a team mode).
	function foes() { return S.view.Players.filter(function (p) { return p.Team !== S.team; }); }
	// allied is whether players stand beside you (a team mode): their units
	// are on your side, drawn in your colour, but not yours to order.
	function allied() { return S.view.Players.some(function (p) { return p.Team === S.team && p.ID !== S.you; }); }
	// tint is how a unit is drawn from here: 0 yours, 1 theirs, 2 an ally's.
	function tint(u) { return rel(u.Team) === 1 ? 1 : u.Owner === undefined || mine(u) ? 0 : 2; }
	function ownerName(u) { return (S.view.Players.filter(function (p) { return p.ID === u.Owner; })[0] || {}).Name || 'an ally'; }
	function visible(p) {
		if (S.replay || S.whole) { return true; } // a replay (or a hotseat turn playing out) shows everything
		var v = S.view.Visible || [];
		for (var i = 0; i < v.length; i++) { if (same(v[i], p)) { return true; } }
		return false;
	}
	function ordersOf(id) { return S.orders.filter(function (o) { return o.UnitID === id; }); }
	function plannedDest(id) {
		var d = null;
		ordersOf(id).forEach(function (o) {
			if (o.Action === 'move' && o.Path && o.Path.length) { d = o.Path[o.Path.length - 1]; }
			// Its own instant jump lands before anything moves or fires.
			var ab = o.Action === 'ability' && C.Abilities[o.Ability];
			if (ab && !ab.Delay && (ab.Effects || []).some(function (e) { return e.Kind === 'teleport' && e.Who !== 'target' && e.To === 'target'; })) { d = o.Target; }
		});
		return d;
	}
	function limit(u) { return u.IsCommander ? C.Rules.CommanderOrders : 1; }

	// layout shapes the board for the match: every square square. A team
	// board is taller than wide on screen, so it takes the full height and
	// less width (--ar is its columns over its rows; play.css sizes it).
	// A board whose squares would come out smaller than CAM.floor (a team
	// board on a phone) gets the camera instead, opening on your squad;
	// keep (a resize) holds the camera where it was.
	function layout(keep) {
		var b = $('board'), g = $('game'), was = CAM.on;
		b.style.gridTemplateColumns = 'repeat(' + cols() + ', 1fr)';
		b.style.gridTemplateRows = 'repeat(' + rows() + ', 1fr)';
		g.style.setProperty('--ar', String(cols() / rows()));
		// a fingertip wants about 27 CSS px a square; a mouse is fine with 22
		CAM.floor = window.matchMedia && window.matchMedia('(pointer: coarse)').matches ? 27 : 22;
		g.classList.remove('cam');
		CAM.on = $('boardwrap').clientWidth / cols() < CAM.floor;
		g.classList.toggle('cam', CAM.on);
		BW = 0;
		render.sq = 0;
		$('mini').classList.add('hidden'); // camApply shows it, in the close view
		delete $('zoom').dataset.close;
		$('zoom').classList.toggle('hidden', !CAM.on);
		if (!CAM.on) { $('cam').style.cssText = ''; return; }
		camSize();
		if (keep && was) { camApply(0); return; }
		CAM.close = true;
		var own = S.view.Units.filter(function (u) { return mine(u) && !u.Dead && u.HP > 0; }).map(function (u) { return u.Pos; });
		var cmd = unit(me().Commander);
		if (cmd && !cmd.Dead) { own.push(cmd.Pos); }
		camShow(own, 0);
	}

	// ---- the camera ------------------------------------------------------
	// #cam carries the board and everything drawn on it, in board pixels at
	// the close square size; one transform on it pans (x, y: the board pixel
	// at the room's top left) and zooms out to the whole board. A board that
	// fits is never moved. Callouts, the minimap and the zoom button belong
	// to the room (#boardwrap), so they stay in view.
	var CAM = { on: false, close: true, x: 0, y: 0, vh: 0, floor: 22 };
	function camSize() {
		var z = sqSize(), c = $('cam');
		c.style.width = cols() * z + 'px';
		c.style.height = rows() * z + 'px';
		drawMini(true);
	}
	// camView is the transform: the close view clamped to the board's edges
	// (centred where the board is smaller than the room), or the whole board
	// fitted and centred.
	function camView() {
		var z = sqSize(), bw = cols() * z, bh = rows() * z, vw = BW, vh = CAM.vh;
		if (!CAM.close) {
			var s = Math.min(vw / bw, vh / bh);
			return { tx: (vw - bw * s) / 2, ty: (vh - bh * s) / 2, s: s };
		}
		var fit = function (pos, size, view) { return size <= view ? (size - view) / 2 : Math.max(0, Math.min(size - view, pos)); };
		CAM.x = fit(CAM.x, bw, vw);
		CAM.y = fit(CAM.y, bh, vh);
		return { tx: -CAM.x, ty: -CAM.y, s: 1 };
	}
	// camApply puts the camera where CAM says, gliding over ms (a cut with
	// reduced motion, or 0).
	function camApply(ms) {
		if (!CAM.on) { return; }
		var c = $('cam'), v = camView();
		c.style.transition = ms && !still() ? 'transform ' + Math.round(ms) + 'ms cubic-bezier(0.3, 0.7, 0.3, 1)' : 'none';
		c.style.transform = 'translate3d(' + v.tx + 'px,' + v.ty + 'px,0) scale(' + v.s + ')';
		var z = sqSize(), m = $('mini'), k = m.k || 1, zb = $('zoom');
		m.lastChild.style.transform = 'translate(' + (CAM.x / z * k) + 'px,' + (CAM.y / z * k) + 'px)';
		if (zb.dataset.close === String(CAM.close)) { return; }
		// the minimap goes with the close view; the button offers the other
		zb.dataset.close = String(CAM.close);
		m.classList.toggle('hidden', !CAM.close);
		zb.setAttribute('aria-label', CAM.close ? 'whole board' : 'close view');
		zb.innerHTML = '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round">' +
			(CAM.close ? '<path d="M2 6V2h4M10 2h4v4M14 10v4h-4M6 14H2v-4"/>' : '<circle cx="7" cy="7" r="4.5"/><path d="M10.5 10.5 14 14M5 7h4M7 5v4"/>') + '</svg>';
	}
	// box is the board pixels squares pts take on screen.
	function box(pts) {
		var z = sqSize(), b = { x0: 1e9, y0: 1e9, x1: -1e9, y1: -1e9 };
		pts.forEach(function (p) {
			var s = toScreen(p);
			b.x0 = Math.min(b.x0, s.c * z); b.y0 = Math.min(b.y0, s.r * z);
			b.x1 = Math.max(b.x1, (s.c + 1) * z); b.y1 = Math.max(b.y1, (s.r + 1) * z);
		});
		return b;
	}
	// fits is whether box b fits the close view with a margin round it.
	function fits(b) { var m = sqSize() * 0.6; return b.x1 - b.x0 <= BW - 2 * m && b.y1 - b.y0 <= CAM.vh - 2 * m; }
	// inView is whether squares pts are all in the close view, clear of its
	// edges (always, in the whole view).
	function inView(pts) {
		if (!CAM.on || !CAM.close || !pts.length) { return true; }
		var b = box(pts), m = sqSize() * 0.6;
		return b.x0 >= CAM.x + m && b.x1 <= CAM.x + BW - m && b.y0 >= CAM.y + m && b.y1 <= CAM.y + CAM.vh - m;
	}
	// camShow brings squares pts into the close view: centred on them, or on
	// the last of them when they are too spread out to show at once.
	function camShow(pts, ms) {
		if (!CAM.on || !CAM.close || !pts.length) { return; }
		var b = box(fits(box(pts)) ? pts : pts.slice(-1));
		CAM.x = (b.x0 + b.x1 - BW) / 2;
		CAM.y = (b.y0 + b.y1 - CAM.vh) / 2;
		camApply(ms);
	}
	// zoom switches between the close view and the whole board; zooming in
	// at a point of the screen (a pinch) centres the close view there.
	function zoom(close, at) {
		if (!CAM.on || CAM.close === close) { return; }
		if (close && at) {
			var r = $('boardwrap').getBoundingClientRect(), v = camView();
			CAM.x = (at.x - r.left - v.tx) / v.s - BW / 2;
			CAM.y = (at.y - r.top - v.ty) / v.s - CAM.vh / 2;
		}
		CAM.close = close;
		camApply(320);
	}
	// focus is the squares an event plays on, the one that matters last.
	function focus(e) {
		var pts = [], u = e.u && unit(e.u), tu = e.tu && unit(e.tu);
		var mid = function (ts) {
			if (!ts || !ts.length) { return; }
			var x = 0, y = 0;
			ts.forEach(function (t) { x += t.X; y += t.Y; });
			pts.push({ X: Math.round(x / ts.length), Y: Math.round(y / ts.length) });
		};
		// (an event's from and to are always sent; they mean something only
		// for the kinds that use them). What plays nothing is not followed.
		if (e.k === 'OrderRejected' || e.why === 'expired' || e.k === 'Overwatch' && !e.n) { return pts; }
		switch (e.k) {
			case 'Moved': pts.push(e.from, e.path && e.path.length ? e.path[e.path.length - 1] : e.to); break;
			case 'Teleported': case 'Pushed': pts.push(e.from, e.to); break;
			case 'Spawned': case 'Respawned': case 'Telegraph': case 'AbilityResolved': pts.push(e.to); break;
			case 'AbilityCast':
				if (u) { pts.push(u.Pos); }
				if (!/^(self|all)$/.test((C.Abilities[e.ab] || {}).Target)) { pts.push(e.to); }
				break;
			case 'Revealed': case 'Smoke': case 'ObjectiveScored': mid(e.tiles); break;
			default:
				if (u) { pts.push(u.Pos); }
				if (tu) { pts.push(tu.Pos); }
		}
		return pts.filter(function (p) { return p && p.X >= 0 && p.Y >= 0 && p.X < W() && p.Y < H(); });
	}
	// follow brings event i into view if it is not, with as many of the
	// events after it as fit beside it, so the camera moves once for a
	// cluster of nearby events rather than for each.
	function follow(events, i, ms) {
		if (!CAM.on || !CAM.close) { return; }
		var pts = focus(events[i]);
		if (inView(pts)) { return; }
		for (var j = i + 1; j < events.length && j < i + 8; j++) {
			var more = pts.concat(focus(events[j]));
			if (!fits(box(more))) { break; }
			pts = more;
		}
		camShow(pts, ms);
		drawMini();
	}
	// drawMini paints the minimap: the board in its colours, the fog darker,
	// objectives in their holder's colour, units as dots of their side's
	// (and the room as a rectangle over it, moved by camApply). Only when
	// something on it changed.
	function drawMini(force) {
		if (!CAM.on) { return; }
		var m = $('mini'), cv = m.firstChild, th = board(PREF.board);
		var key = boardSig + th[0] + S.view.Units.map(function (u) { return u.Dead || u.HP <= 0 ? '' : u.Pos.X + ',' + u.Pos.Y; }).join(' ');
		if (!force && m.key === key) { return; }
		m.key = key;
		var k = m.k = Math.max(2, Math.floor(96 / Math.max(cols(), rows()))), dpr = Math.min(2, window.devicePixelRatio || 1), q = k * dpr, z = sqSize();
		cv.width = cols() * q;
		cv.height = rows() * q;
		cv.style.width = cols() * k + 'px';
		cv.style.height = rows() * k + 'px';
		m.lastChild.style.width = Math.min(cols() * k, BW / z * k) + 'px';
		m.lastChild.style.height = Math.min(rows() * k, CAM.vh / z * k) + 'px';
		var g = cv.getContext('2d'), seen = {}, all = S.replay || S.whole;
		(S.view.Visible || []).forEach(function (v) { seen[v.X + ',' + v.Y] = true; });
		var sq = function (p, color, inset) { var s = toScreen(p), d = inset ? Math.max(1, Math.round(q / 5)) : 0; g.fillStyle = color; g.fillRect(s.c * q + d, s.r * q + d, q - 2 * d, q - 2 * d); };
		for (var r = 0; r < rows(); r++) {
			for (var c = 0; c < cols(); c++) {
				var p = toBoard(c, r);
				sq(p, tile(p).t === 'wall' ? th[3] : (p.X + p.Y) % 2 ? th[2] : th[1]);
				if (!all && !seen[p.X + ',' + p.Y]) { sq(p, 'rgba(0,0,0,0.55)'); }
			}
		}
		(S.view.Objectives || []).forEach(function (o) {
			var col = o.Holder < 0 ? 'rgba(230,240,245,0.45)' : alpha(o.Holder === S.team ? TEAM[0][0] : TEAM[1][0], 0.6);
			o.Tiles.forEach(function (t) { sq(t, col); });
		});
		S.view.Units.forEach(function (u) { if (!u.Dead && u.HP > 0) { sq(u.Pos, ['#4fd3e8', '#ff7043', '#2a8fa0'][tint(u)], true); } });
	}

	// ---- drawing --------------------------------------------------------
	function render() {
		measure();
		// a square's size, for effects sized in CSS (numbers, callouts)
		if (render.sq !== sqSize()) {
			render.sq = sqSize();
			$('boardwrap').style.setProperty('--sq', render.sq + 'px');
			if (CAM.on) { camSize(); camApply(0); }
		}
		$('game').classList.toggle('busy', !!S.busy);
		renderBoard();
		drawMini();
		renderPieces();
		renderMarks();
		renderPanel();
		renderActions();
		renderTop();
		renderSide();
		renderCoach();
	}
	// renderSide fills the desktop column: the squad, then last turn's log.
	// (Hidden on a phone, where the board takes the room.)
	function renderSide() {
		var ro = $('roster');
		var squad = S.view.Units.filter(function (u) { return mine(u); });
		var key = squad.map(function (u) {
			return [u.ID, u.Kind, u.HP, u.MaxHP, !!u.Dead, u.RespawnIn || 0, ordersOf(u.ID).length].join(',');
		}).join('|');
		if (ro.dataset.key !== key) {
			ro.dataset.key = key;
			ro.textContent = '';
			squad.forEach(function (u) {
				var row = document.createElement('button');
				var dead = u.Dead || u.HP <= 0;
				row.className = 'mate' + (dead ? ' down' : '');
				row.dataset.id = u.ID;
				var sp = document.createElement('div');
				row.appendChild(sp);
				sprite(sp, spriteID(u), 40, rel(u.Team));
				var f = u.MaxHP ? Math.max(0, u.HP) / u.MaxHP : 0;
				var os = ordersOf(u.ID).length, lim = limit(u), pips = '';
				for (var i = 0; i < lim; i++) { pips += i < os ? '●' : '○'; }
				var info = document.createElement('div');
				info.className = 'mi';
				info.innerHTML = '<b>' + (u.IsCommander ? (C.Heroes[u.Kind] || {}).Name || u.Name : u.Name) + '</b>' +
					'<span class="bar"><i class="' + (f <= 1 / 3 ? 'bad' : f <= 2 / 3 ? 'warn' : '') + '" style="width:' + f * 100 + '%"></i></span>' +
					'<small>' + (dead ? (u.RespawnIn ? 'back in ' + u.RespawnIn : 'down') : u.HP + '/' + u.MaxHP) + '</small>';
				row.appendChild(info);
				var pp = document.createElement('span'); pp.className = 'pips'; pp.textContent = dead ? '' : pips;
				row.appendChild(pp);
				// the unit as it is when tapped: this row can outlive a turn
				row.onclick = function () { if (!S.busy) { select(u.ID === S.sel ? null : unit(u.ID)); } };
				ro.appendChild(row);
			});
		}
		each(ro, '.mate', function (row) {
			row.classList.toggle('on', row.dataset.id === String(S.sel));
			row.disabled = row.classList.contains('down') || !!S.busy;
		});
		var lg = $('log');
		if (lg.shown === S.log) { return; }
		lg.shown = S.log;
		lg.textContent = '';
		(S.log || []).forEach(function (l) {
			var li = document.createElement('li');
			li.className = l.c || '';
			li.textContent = l.t;
			lg.appendChild(li);
		});
	}
	// logLines turns a turn's events into short lines, as the terminal's log
	// tells it, for the desktop column.
	function logLines(events, view) {
		var name = function (id) {
			var u = null;
			view.Units.forEach(function (x) { if (x.ID === id) { u = x; } });
			if (!u) { return 'someone'; }
			return ['', 'enemy ', 'ally '][tint(u)] + (u.IsCommander ? (C.Heroes[u.Kind] || {}).Name || u.Name : u.Name);
		};
		var side = function (id) { var u = null; view.Units.forEach(function (x) { if (x.ID === id) { u = x; } }); return u && rel(u.Team) === 0 ? 'own' : 'enemy'; };
		var out = [];
		events.forEach(function (e) {
			switch (e.k) {
				case 'Attacked': out.push({ t: name(e.u) + ' → ' + name(e.tu) + (e.hits ? '' : ': miss'), c: side(e.u) }); break;
				case 'Damaged': out.push({ t: '  ' + name(e.tu) + ' −' + (e.n || 0), c: 'dmg' }); break;
				case 'Healed': out.push({ t: '  ' + name(e.tu || e.u) + ' +' + (e.n || 0) + (e.n ? '' : ' (full)'), c: 'heal' }); break;
				case 'Died': out.push({ t: name(e.u) + ' down', c: 'dmg' }); break;
				case 'AbilityCast': out.push({ t: name(e.u) + ': ' + ((C.Abilities[e.ab] || {}).Name || e.ab), c: side(e.u) }); break;
				case 'ObjectiveScored': out.push({ t: '+' + e.n + ' ' + (rel(e.team || 0) === 0 ? (allied() ? 'your team' : 'you') : 'them') + ' (' + (e.why || '') + ')', c: rel(e.team || 0) === 0 ? 'own' : 'enemy' }); break;
				case 'LevelUp': out.push({ t: name(e.u) + ' reaches level ' + e.n, c: side(e.u) }); break;
			}
		});
		return out.length ? out.slice(-14) : [{ t: 'a quiet turn', c: '' }];
	}
	function renderTop() {
		var v = S.view;
		var cmd = unit(me().Commander);
		$('oppname').textContent = (foes().length > 1 ? who(1) : them().Name || 'opponent') + (S.replay ? (S.replay.live ? ' · live' : ' · replay') : S.online ? ' · ' + S.online.time.replace(/-casual$/, '') : S.hotseat ? '' : ' · ' + pick.level);
		$('turn').textContent = 'turn ' + v.Match.Turn + '/' + v.Match.MaxTurns;
		$('oppscore').innerHTML = bars(v.Teams[1 - S.team].Score, v.Match.WinScore, 'enemy');
		$('myname').textContent = (allied() ? 'your team' : S.replay && S.replay.live || S.hotseat ? me().Name : 'you') + ' · ' + (cmd ? cmd.Name : '');
		$('goal').innerHTML = 'first to ' + v.Match.WinScore + forecast();
		$('myscore').innerHTML = bars(v.Teams[S.team].Score, v.Match.WinScore, 'own');
		renderInfo();
	}
	// status is what this moment of the turn asks of you.
	function status() {
		if (S.replay && S.replay.live) { return 'Live · turn ' + S.view.Match.Turn + (S.replay.delayed ? ' (a turn behind)' : ''); }
		if (S.replay) { return 'Replay · turn ' + S.replay.i + ' of ' + S.replay.n; }
		if (S.hotseat && !S.busy) { return me().Name + ': plan your orders' + (S.orders.length ? ' (' + S.orders.length + ' given)' : ''); }
		if (S.busy) { return 'Resolving the turn…'; }
		if (S.online && S.online.committed) { return 'Orders in · waiting for the other side'; }
		var n = S.orders.length;
		return 'Your turn: plan your orders' + (n ? ' (' + n + ' given)' : '');
	}
	// renderInfo fills the match box beside the board (desktop): what kind of
	// match, who is in it, and what the turn wants.
	function renderInfo() {
		var el = $('matchinfo');
		if (!el) { return; }
		var v = S.view, qn = { casual: 'Casual', blitz: 'Blitz', bullet: 'Bullet', rapid: 'Rapid', daily: 'Daily' };
		var nf = foes().length, team = allied();
		var kind = S.replay ? (S.replay.live ? 'Live · ' : 'Replay · ') + S.replay.title : S.online ? (qn[S.online.time] || clockLabel(S.online.time)) + ' · online' : S.hotseat ? 'Pass and play' : 'vs ' + (nf > 1 ? nf + ' bots' : 'bot') + ' · ' + pick.level;
		var rows2 = v.Players.map(function (p) {
			var cmd = v.Units.filter(function (u) { return u.ID === p.Commander; })[0];
			var cn = cmd ? ((C.Heroes[cmd.Kind] || {}).Name || cmd.Name) : '';
			var ally = team && p.Team === S.team && p.ID !== S.you;
			return '<li><i class="dot ' + (rel(p.Team) === 0 ? 'own' : 'enemy') + '"></i>' + (p.ID === S.you && !S.hotseat && !(S.replay && S.replay.live) ? 'you' : p.Name) + (cn ? ' <span class="dim">' + cn + (ally ? ' · ally' : '') + '</span>' : '') + '</li>';
		}).join('');
		el.innerHTML = '<div class="mi-kind">' + kind + '</div><div class="dim">' + v.Match.Mode + (team ? ' (experimental)' : '') + ' · first to ' + v.Match.WinScore + ' · ' + v.Match.MaxTurns + ' turns</div>' +
			'<ul>' + rows2 + '</ul><div class="mi-status">' + status() + '</div>';
	}
	// forecast says who scores at the end of next turn if no objective
	// changes hands: under net scoring only the side holding more objectives
	// scores, by the difference.
	function forecast() {
		var n = [0, 0];
		(S.view.Objectives || []).forEach(function (o) { if (o.Holder === 0 || o.Holder === 1) { n[rel(o.Holder)]++; } });
		var lead = n[0] - n[1], they = S.online ? 'them' : foes().length > 1 ? 'bots' : 'bot';
		if (C.Rules.Scoring !== 'net') {
			return (n[0] ? ' · <span class="own">you +' + n[0] + '</span>' : '') + (n[1] ? ' · <span class="enemy">' + they + ' +' + n[1] + '</span>' : '');
		}
		if (lead > 0) { return ' · <span class="own">you +' + lead + '/turn</span>'; }
		if (lead < 0) { return ' · <span class="enemy">' + they + ' +' + (-lead) + '/turn</span>'; }
		return ' · even';
	}
	// bars draws a score as signal strength: one bar per point to win
	// (thinner when a long match plays to more than ten).
	function bars(score, of, cls) {
		var h = '<span class="bars ' + cls + (of > 10 ? ' many' : '') + '">';
		for (var i = 0; i < of; i++) {
			h += '<i class="' + (i < score ? 'on' : '') + '" style="height:' + (30 + 70 * (i + 1) / of) + '%"></i>';
		}
		return h + '</span>' + score;
	}
	// renderBoard rebuilds the floor only when something on it has changed, so
	// a tap in the planning phase leaves it alone and its light keeps running.
	var boardSig = '';
	function renderBoard() {
		var b = $('board');
		var sig = JSON.stringify([S.view.Match.ID, S.view.Match.Turn, S.team, !!S.replay, !!S.whole, cols(), rows(), sqSize(), PREF.board,
			S.view.Visible, S.view.Delayed, S.view.Effects, S.view.Objectives, S.last, S.scars,
			S.view.Units.map(function (u) { return [u.ID, u.Pos, u.Dead]; })]);
		if (sig === boardSig && b.firstChild) { return; }
		boardSig = sig;
		b.textContent = '';
		var tele = {};
		(S.view.Delayed || []).forEach(function (d) { tele[d.Target.X + ',' + d.Target.Y] = true; });
		var smoke = {};
		(S.view.Effects || []).forEach(function (fx) {
			if (fx.Kind === 'smoke') { fx.Tiles.forEach(function (t) { smoke[t.X + ',' + t.Y] = true; }); }
		});
		for (var r = 0; r < rows(); r++) {
			for (var c = 0; c < cols(); c++) {
				var p = toBoard(c, r), t = tile(p);
				var d = document.createElement('div');
				d.className = 'sq ' + ((p.X + p.Y) % 2 ? 'dark' : 'light');
				// Floor plates with wear, fixed per square: seams, bolts, a grate,
				// a crack, a cable trench, or plain. Raised ground is tread plate;
				// your back ranks carry a faint stripe of your colour.
				if (t.t !== 'wall') {
					var wear = ((p.X * 73856093) ^ (p.Y * 19349663)) >>> 0;
					var dv = [0, 0, 0, 3, 4, 5, 6, 7, 8][wear % 9];
					if (dv) { var dc = document.createElement('span'); dc.className = 'decal d' + dv; d.appendChild(dc); }
					if (t.z > 0) { var tr = document.createElement('span'); tr.className = 'tread'; d.appendChild(tr); }
					var dcols = S.view.Board.DeployCols || 0;
					if (S.team === 0 ? p.X < dcols : p.X >= W() - dcols) { var dp = document.createElement('span'); dp.className = 'deploy'; d.appendChild(dp); }
				}
				// Walls are hardware, not holes: a rack, a conduit or a pillar,
				// fixed per square so the board looks the same every turn.
				if (t.t === 'wall') {
					// a block standing on its square: lit top, front face, detail
					d.classList.add('wall');
					var blk = document.createElement('span');
					blk.className = 'block w' + ((p.X * 7 + p.Y * 3) % 3);
					d.appendChild(blk);
				}
				if (t.t === 'cover') {
					var cv = document.createElement('span'); cv.className = 'barrier'; d.appendChild(cv);
				}
				if (t.z > 0 && t.t !== 'wall') {
					d.classList.add('z' + t.z);
					var z = document.createElement('span'); z.className = 'z'; z.textContent = t.z; d.appendChild(z);
				}
				// Height as depth: a ledge where this square drops to the one in
				// front of it (below on screen), a cast shadow where the one behind
				// it (above on screen) stands higher.
				var front = r + 1 < rows() ? tile(toBoard(c, r + 1)) : null;
				var behind = r > 0 ? tile(toBoard(c, r - 1)) : null;
				if (t.t !== 'wall' && front && front.t !== 'wall' && t.z > front.z) {
					var lg = document.createElement('span'); lg.className = 'ledge'; lg.style.height = (6 + 5 * (t.z - front.z)) + '%'; d.appendChild(lg);
				}
				if (t.t !== 'wall' && behind && (behind.t === 'wall' || behind.z > t.z)) {
					var cs = document.createElement('span'); cs.className = 'cast'; d.appendChild(cs);
				}
				if (!visible(p)) { d.classList.add('fog'); }
				if (tele[p.X + ',' + p.Y]) { d.classList.add('tele'); }
				if (smoke[p.X + ',' + p.Y]) { var hz = document.createElement('span'); hz.className = 'haze'; d.appendChild(hz); }
				if (S.last[p.X + ',' + p.Y]) { d.classList.add('last'); }
				if (S.scars[p.X + ',' + p.Y]) { d.appendChild(scorch()); }
				// Coordinates on the edge squares, as a chess board has them; the
				// terminal names squares the same way (columns 1-8, rows a-h from
				// your side).
				if (r === rows() - 1) { var fl = document.createElement('span'); fl.className = 'coord file'; fl.textContent = c + 1; d.appendChild(fl); }
				if (c === 0) { var rk = document.createElement('span'); rk.className = 'coord rank'; rk.textContent = 'abcdefghijklmnopqrstuvwxyz'.charAt(rows() - 1 - r); d.appendChild(rk); }
				b.appendChild(d);
			}
		}
		fog(b);
		network(b);
		(S.view.Objectives || []).forEach(function (o) { b.appendChild(zone(o)); });
		var vg = document.createElement('div'); vg.className = 'vignette'; b.appendChild(vg);
	}
	// network lays the fibre under the floor: a run from each objective to
	// the nearest side of the board, and from the core out to the others,
	// with light pulsing along them toward the nodes. A held node's runs,
	// and the pool of light it casts, take its holder's colour.
	function network(b) {
		var objs = S.view.Objectives || [];
		if (!objs.length) { return; }
		var z = sqSize(), wpx = cols() * z, NS = 'http://www.w3.org/2000/svg';
		var svg = document.createElementNS(NS, 'svg');
		svg.setAttribute('class', 'fibre');
		var mid = function (o) {
			var x = 0, y = 0;
			o.Tiles.forEach(function (t) { var c = center(t); x += c.x; y += c.y; });
			return { x: x / o.Tiles.length, y: y / o.Tiles.length };
		};
		var colorOf = function (o) { return o.Holder < 0 ? '170,215,230' : rel(o.Holder) === 0 ? '79,211,232' : '255,112,67'; };
		var run = function (pts, rgb, held) {
			var d = pts.map(function (q, i) { return (i ? 'L' : 'M') + q.x.toFixed(1) + ' ' + q.y.toFixed(1); }).join(' ');
			var base = document.createElementNS(NS, 'path');
			base.setAttribute('d', d);
			base.setAttribute('class', 'cable');
			base.setAttribute('stroke', 'rgba(' + rgb + ',' + (held ? 0.32 : 0.16) + ')');
			svg.appendChild(base);
			var pulse = document.createElementNS(NS, 'path');
			pulse.setAttribute('d', d);
			pulse.setAttribute('class', 'pulse-run');
			pulse.setAttribute('stroke', 'rgba(' + rgb + ',' + (held ? 0.85 : 0.55) + ')');
			svg.appendChild(pulse);
		};
		var core = objs.reduce(function (a, o) { return o.Tiles.length > a.Tiles.length ? o : a; }, objs[0]);
		var cm = mid(core);
		objs.forEach(function (o) {
			var m = mid(o), held = o.Holder === 0 || o.Holder === 1;
			// to the nearest side, along the row
			var edge = m.x < wpx / 2 ? 0 : wpx;
			run([{ x: edge, y: m.y }, m], colorOf(o), held);
			// from the core out, around the corner
			if (o !== core) { run([cm, { x: cm.x, y: m.y }, m], colorOf(o), held); }
			// a pool of light on the floor
			var pool = document.createElement('div');
			pool.className = 'pool';
			var size = z * (1.6 + Math.sqrt(o.Tiles.length));
			pool.style.left = (m.x - size / 2) + 'px';
			pool.style.top = (m.y - size / 2) + 'px';
			pool.style.width = pool.style.height = size + 'px';
			pool.style.background = 'radial-gradient(closest-side, rgba(' + colorOf(o) + ',' + (held ? 0.26 : 0.16) + '), transparent)';
			b.appendChild(pool);
		});
		b.appendChild(svg);
	}
	// fog lays the fog over the floor, one canvas for the board: each square
	// you cannot see stays its own colours under a Bayer dither of the
	// board's wall colour, sparse (a quarter of the cells) next to what you
	// see, half a square further, dense (three quarters) beyond. The cells
	// are the figures' pixels; the tiles are built once a theme and size.
	var FOG = null;
	function fog(b) {
		if (S.replay || S.whole) { return; }
		var z = sqSize(), n = cols(), m = rows(), seen = {}, near = [];
		(S.view.Visible || []).forEach(function (v) { var s = toScreen(v); seen[s.r * n + s.c] = true; });
		var dist = function (c, r) {
			for (var d = 1; d < 3; d++) {
				for (var y = Math.max(0, r - d); y <= Math.min(m - 1, r + d); y++) {
					for (var x = Math.max(0, c - d); x <= Math.min(n - 1, c + d); x++) { if (seen[y * n + x]) { return d; } }
				}
			}
			return 3;
		};
		for (var r = 0; r < m; r++) {
			for (var c = 0; c < n; c++) { if (!seen[r * n + c]) { near.push([c, r, dist(c, r)]); } }
		}
		if (!near.length) { return; }
		var cv = FOG || (FOG = document.createElement('canvas'));
		cv.className = 'fog';
		cv.width = Math.round(n * z);
		cv.height = Math.round(m * z);
		// two of the figures' pixels to a cell, and a wash of the wall colour
		// under the pattern: on a phone a fine dither over the bare board
		// was glare, the lit holes louder than anything in the fog
		var g = cv.getContext('2d'), wall = board(PREF.board)[3], cell = Math.max(3, Math.round(z * 2.24 / ART.size));
		near.forEach(function (q) {
			var x0 = Math.round(q[0] * z), y0 = Math.round(q[1] * z), w = Math.round((q[0] + 1) * z) - x0, h = Math.round((q[1] + 1) * z) - y0;
			g.globalAlpha = 0.3 + 0.1 * q[2];
			g.fillStyle = wall;
			g.fillRect(x0, y0, w, h);
			g.globalAlpha = 1;
			g.fillStyle = pattern(g, wall, q[2] * 4, cell);
			g.fillRect(x0, y0, w, h);
		});
		b.appendChild(cv);
	}
	function scorch() { var m = document.createElement('span'); m.className = 'scorch'; return m; }
	// zone draws an objective as one outlined area over its tiles, tinted by
	// who holds it, with a ripple from its centre: the thing to fight over
	// should be the first thing the eye finds.
	function zone(o) {
		var z = sqSize(), c0 = 1e9, r0 = 1e9, c1 = -1, r1 = -1;
		o.Tiles.forEach(function (t) {
			var s = toScreen(t);
			c0 = Math.min(c0, s.c); r0 = Math.min(r0, s.r); c1 = Math.max(c1, s.c); r1 = Math.max(r1, s.r);
		});
		// Held by one side, or contested: units in it but nobody holds it
		// (both sides are in it, possibly one hidden in the fog).
		var occupied = o.Tiles.some(function (t) { return unitAt(t); });
		var el = document.createElement('div');
		el.className = 'zone' + (o.Holder >= 0 ? (rel(o.Holder) === 0 ? ' own' : ' enemy') : occupied ? ' contested' : '');
		if (o.Holder < 0 && occupied) {
			var tag = document.createElement('b');
			tag.textContent = 'contested';
			el.appendChild(tag);
		}
		el.style.left = (c0 * z) + 'px';
		el.style.top = (r0 * z) + 'px';
		el.style.width = ((c1 - c0 + 1) * z) + 'px';
		el.style.height = ((r1 - r0 + 1) * z) + 'px';
		var ring = document.createElement('i');
		ring.style.width = ring.style.height = (z * 0.9) + 'px';
		el.appendChild(ring);
		return el;
	}
	// A piece sits where its transform says, and moves by animating that: the
	// browser can run it off the main thread, and nothing has to be laid out.
	function xform(p, z) { var s = toScreen(p); return 'translate3d(' + (s.c * z) + 'px,' + (s.r * z) + 'px,0)'; }
	function pieceKey(u, z) { return [u.ID, spriteID(u), tint(u), z].join('|'); }
	function pieceEl(u, opts) {
		opts = opts || {};
		var z = sqSize();
		var el = document.createElement('div');
		el.className = 'piece ' + (rel(u.Team) === 0 ? 'own' : 'enemy') + (tint(u) === 2 ? ' ally' : '') + (u.IsCommander ? ' cmd' : '');
		el.style.width = el.style.height = z + 'px';
		el.style.transform = xform(opts.at || u.Pos, z);
		el.dataset.id = u.ID;
		el.dataset.key = pieceKey(u, z);
		if (!opts.ghost) {
			var sh = document.createElement('span'); sh.className = 'shadow';
			el.appendChild(sh);
		}
		if (ART && ART.floating && ART.floating[spriteID(u)]) { el.classList.add('floats'); }
		var body = document.createElement('div'); body.className = 'body';
		var sp = document.createElement('div');
		body.appendChild(sp);
		el.appendChild(body);
		sprite(sp, spriteID(u), Math.floor(z * 1.12), tint(u), { outline: true, breath: !opts.ghost });
		if (!opts.ghost) {
			// Segmented, one tick per hit point; the light "lag" behind the fill
			// drains a moment after a hit, so the damage reads as an amount.
			var hp = document.createElement('div'); hp.className = 'hp';
			hp.style.setProperty('--seg', (100 / Math.max(1, u.MaxHP)) + '%');
			var lag = document.createElement('b'); lag.className = 'lag';
			var fill = document.createElement('i');
			hp.appendChild(lag);
			hp.appendChild(fill);
			el.appendChild(hp);
			setHP(el, u);
			// A slow two-frame idle, out of step with its neighbours.
			sp.style.setProperty('--d', (-(u.ID * 0.37) % 2.4) + 's');
		}
		return el;
	}
	// setHP shows a unit's health on its piece. The bar drains (a scale, so
	// nothing is laid out) and the light lag follows it down a moment later.
	function setHP(el, u) {
		var fill = el.querySelector('.hp i'), lag = el.querySelector('.hp .lag');
		if (!fill) { return; }
		var f = u.MaxHP ? Math.max(0, u.HP) / u.MaxHP : 0;
		fill.style.transform = 'scaleX(' + f + ')';
		if (lag) { lag.style.transform = 'scaleX(' + f + ')'; }
		fill.className = f <= 1 / 3 ? 'bad' : f <= 2 / 3 ? 'warn' : '';
		el.classList.toggle('hurt', f <= 1 / 3);
	}
	// renderPieces keeps each unit's element from one render to the next: its
	// animations run on, and nothing is drawn again that has not changed.
	function renderPieces() {
		var box = $('pieces'), z = sqSize(), live = {};
		Array.prototype.slice.call(box.children).forEach(function (el) {
			if (el.classList.contains('ghost') || el.dataset.gone) { el.remove(); } else { live[el.dataset.key] = el; }
		});
		var first = box.firstChild;
		var under = function (g) { g.classList.add('ghost'); box.insertBefore(g, first); };
		// Where planned moves and jumps end: a faded copy of the unit (a
		// pre-move); the one just ordered drops into place.
		var placed = {};
		S.orders.forEach(function (o) {
			var u = unit(o.UnitID), d = u && !placed[u.ID] && plannedDest(u.ID);
			if (!d) { return; }
			placed[u.ID] = true;
			var g = pieceEl(u, { at: d, ghost: true });
			if (u.ID === S.fresh) { g.classList.add('fresh'); }
			under(g);
		});
		S.fresh = 0;
		// Enemies last seen where they went out of sight.
		(S.view.Sightings || []).forEach(function (sg) {
			if (sg.Team !== S.team || unitAt(sg.Pos) || visible(sg.Pos)) { return; }
			var g = pieceEl({ ID: -sg.UnitID, Team: 1 - S.team, Kind: sg.Kind, IsCommander: !!C.Heroes[sg.Kind], Pos: sg.Pos, HP: 0, MaxHP: 0 }, { ghost: true });
			g.classList.add('lastseen');
			under(g);
		});
		S.view.Units.forEach(function (u) {
			if (u.Dead || u.HP <= 0) { return; }
			var key = pieceKey(u, z), el = live[key];
			if (el) {
				delete live[key];
				el.style.transform = xform(u.Pos, z);
				el.classList.remove('drag');
				var sp = el.querySelector('.body .sprite');
				if (sp) { pose(sp, false); }
				setHP(el, u);
			} else {
				el = pieceEl(u);
				box.appendChild(el);
			}
			el.classList.toggle('sel', u.ID === S.sel);
			el.classList.toggle('done', mine(u) && ordersOf(u.ID).length >= limit(u));
			// the unit in hand stands in light that falls off in dither steps:
			// one canvas, moved to whichever piece is selected
			if (u.ID === S.sel) {
				var cell = Math.max(2, Math.round(z * 1.12 / ART.size)), n = Math.ceil(z / cell);
				var gl = pool('glow|' + z, n, n, cell, 'rgba(79,211,232,0.5)', function (r) { return step(r, 0.4, 0.65, 0.9); }, 1, 0.5, 0.62);
				if (gl.parentNode !== el) { gl.classList.add('glow'); el.insertBefore(gl, el.firstChild); }
			}
		});
		each(box, '.glow', function (g) { if (!g.parentNode.classList.contains('sel')) { g.remove(); } });
		Object.keys(live).forEach(function (k) { live[k].remove(); });
	}
	function mark(cls, p, sizeFrac) {
		var c = center(p), z = sqSize();
		var m = document.createElement('div');
		m.className = 'mark ' + cls;
		m.style.left = c.x + 'px';
		m.style.top = c.y + 'px';
		if (sizeFrac) { m.style.width = m.style.height = (z * sizeFrac) + 'px'; }
		return m;
	}
	// Marks ripple out from the unit when it is picked up (each one a beat
	// later the further it is), and only then: a redraw leaves them still.
	var marksFor = '';
	function renderMarks() {
		var fx = $('fx');
		Array.prototype.slice.call(fx.querySelectorAll('.mark')).forEach(function (m) { m.remove(); });
		var svg = $('lines');
		svg.innerHTML = '';
		var key = [S.sel, S.abil, S.orders.length].join('|'), enter = key !== marksFor;
		marksFor = key;
		var put = function (m, i) {
			if (enter) { m.classList.add('enter'); m.style.setProperty('--i', i); }
			fx.appendChild(m);
			return m;
		};
		if (S.abil) {
			S.abilTargets.forEach(function (t) { put(mark('ability', t.at, 0.86), 2); });
		} else {
			S.moves.forEach(function (mv) { put(mark('move', mv.to, 0.2), (mv.path || []).length); });
			S.targets.forEach(function (t) {
				put(mark('target', t.at, 0.86), 3);
				var odds = mark('odds', t.at);
				odds.style.top = (center(t.at).y + sqSize() * 0.22) + 'px';
				odds.textContent = t.dice + '×' + t.tn + '+';
				fx.appendChild(odds);
			});
		}
		// The selected unit's planned orders, drawn as arrows.
		if (S.sel) {
			ordersOf(S.sel).forEach(function (o) {
				var u = unit(o.UnitID);
				if (!u) { return; }
				// A planned move shows as the faded unit on its destination; only
				// attacks, which have no other mark, get a line.
				if (o.Action === 'attack') {
					arrow(svg, [plannedDest(u.ID) || u.Pos, o.Target], 'rgba(255,112,67,0.85)');
				}
			});
		}
	}
	function arrow(svg, pts, color) {
		var d = pts.map(function (p, i) { var c = center(p); return (i ? 'L' : 'M') + c.x + ' ' + c.y; }).join(' ');
		var path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
		path.setAttribute('d', d);
		path.setAttribute('fill', 'none');
		path.setAttribute('stroke', color);
		path.setAttribute('stroke-width', Math.max(3, sqSize() * 0.08));
		path.setAttribute('stroke-linecap', 'round');
		path.setAttribute('stroke-linejoin', 'round');
		svg.appendChild(path);
		var end = center(pts[pts.length - 1]);
		var dot = document.createElementNS('http://www.w3.org/2000/svg', 'circle');
		dot.setAttribute('cx', end.x);
		dot.setAttribute('cy', end.y);
		dot.setAttribute('r', Math.max(4, sqSize() * 0.1));
		dot.setAttribute('fill', color);
		svg.appendChild(dot);
	}

	// The unit under the board: whoever was last tapped or selected.
	function renderPanel() {
		var u = unit(S.look || S.sel) || unit(me().Commander);
		var pt = $('portrait'), info = $('info');
		if (!u || u.Dead) { pt.textContent = ''; pt.dataset.key = ''; pt.classList.add('static'); info.textContent = ''; return; }
		pt.classList.remove('static');
		var pkey = [u.ID, spriteID(u), tint(u), pt.clientWidth].join('|');
		if (pt.dataset.key !== pkey) {
			pt.dataset.key = pkey;
			pt.textContent = '';
			var sp = document.createElement('div');
			pt.appendChild(sp);
			sprite(sp, spriteID(u), Math.max(48, pt.clientWidth - 8), tint(u));
		}
		var name = u.IsCommander ? (C.Heroes[u.Kind] ? C.Heroes[u.Kind].Name : u.Name) + ' L' + u.Level : u.Name;
		var f = u.MaxHP ? u.HP / u.MaxHP : 0;
		var html = '<div class="nm ' + (rel(u.Team) === 0 ? 'own' : 'enemy') + '">' + name + (tint(u) === 2 ? ' <small class="dim">ally · ' + ownerName(u) + ' orders it</small>' : '') + '</div>' +
			'<div><span class="bar"><i style="width:' + (f * 100) + '%"></i></span> ' + u.HP + '/' + u.MaxHP + '</div>' +
			'<div>MV ' + u.MV + ' · RNG ' + u.RNG + ' · ATK ' + u.ATK + ' · DEF ' + u.DEF + ' · INI ' + u.INI + '</div>';
		if (mine(u)) {
			var os = ordersOf(u.ID).map(function (o) { return o.Action === 'ability' ? (C.Abilities[o.Ability] || {}).Name : o.Action; });
			html += '<div>orders: ' + (os.length ? os.join(', ') : '—') + ' (' + os.length + '/' + limit(u) + ')</div>';
		}
		info.innerHTML = html;
	}

	function renderActions() {
		var nav = $('actions');
		nav.textContent = '';
		nav.classList.remove('one', 'six');
		if (S.replay && S.replay.live) {
			var lb = document.createElement('button');
			lb.innerHTML = 'Leave<small>' + (S.replay.ended ? S.replay.ended : 'watching live' + (S.replay.delayed ? ', a turn behind' : '')) + '</small>';
			lb.onclick = closeReplay;
			nav.classList.add('one');
			nav.appendChild(lb);
			return;
		}
		if (S.replay) { replayControls(nav); return; }
		var u = unit(S.sel);
		var btn = function (label, sub, on, cls, off) {
			var b = document.createElement('button');
			b.innerHTML = label + (sub ? '<small>' + sub + '</small>' : '');
			if (cls) { b.className = cls; }
			b.disabled = !!off || S.busy;
			b.onclick = on;
			nav.appendChild(b);
		};
		var hasAbilities = u && mine(u) && abilitiesOf(u).length > 0;
		btn('Abilities', hasAbilities ? '' : 'select a unit', openAbilities, '', !hasAbilities);
		btn('Hold', '+DEF', function () { if (u) { addOrder({ UnitID: u.ID, Action: 'hold' }); } }, '', !(u && mine(u)));
		btn('Watch', 'overwatch', function () { if (u) { addOrder({ UnitID: u.ID, Action: 'overwatch' }); } }, '', !(u && mine(u)));
		btn('Undo', u && mine(u) ? 'this unit' : 'last order', undo, '', !S.orders.length);
		if (S.online && S.online.committed) {
			btn('Waiting', 'for the other side', function () {}, 'go', true);
		} else {
			// every unit has its order: the button lights up to be pressed
			var ready = S.orders.length && S.view.Units.every(function (x) { return !mine(x) || x.Dead || x.HP <= 0 || ordersOf(x.ID).length; });
			btn('End turn', S.orders.length + ' orders', endTurn, 'go' + (ready ? ' ready' : ''));
		}
	}

	// ---- selecting and ordering -----------------------------------------
	function select(u, quiet) {
		S.sel = u ? u.ID : 0;
		S.look = S.sel;
		S.abil = null;
		S.abilTargets = [];
		S.moves = [];
		S.targets = [];
		if (u && mine(u) && ordersOf(u.ID).length < limit(u)) {
			var dest = plannedDest(u.ID);
			// the orders already planned go along, so a path keeps off the
			// squares your other units are ordered into
			if (!dest) { S.moves = call('moves', u.ID, JSON.stringify(S.orders)) || []; }
			var from = dest || u.Pos;
			var attacked = ordersOf(u.ID).some(function (o) { return o.Action === 'attack'; });
			if (!attacked) { S.targets = call('targets', u.ID, from.X, from.Y) || []; }
		}
		if (!quiet) { render(); }
	}
	function toast(msg) {
		$('toast').textContent = msg || '';
		clearTimeout(toast.t);
		if (msg) { toast.t = setTimeout(function () { $('toast').textContent = ''; }, 2500); }
	}
	// addOrder plans an order, replacing one of the same kind for the unit
	// (a unit has one order, a commander two), after the engine agrees.
	function addOrder(o) {
		var u = unit(o.UnitID);
		var kept = S.orders.filter(function (x) {
			if (x.UnitID !== o.UnitID) { return true; }
			return limit(u) > 1 && x.Action !== o.Action;
		});
		var mineNow = kept.filter(function (x) { return x.UnitID === o.UnitID; });
		while (mineNow.length >= limit(u)) {
			kept.splice(kept.indexOf(mineNow.shift()), 1);
		}
		kept.push(o);
		var errs = call('check', JSON.stringify(kept)) || [];
		if (errs.length) { toast(errs[0]); return false; }
		S.orders = kept;
		keep(ORDERS, JSON.stringify({ turn: S.view.Match.Turn, orders: S.orders }));
		toast('');
		// the order lands: a ring where it will happen, the ghost drops in
		S.fresh = u.ID;
		var at = o.Action === 'move' ? o.Path[o.Path.length - 1] : o.Action === 'attack' || o.Action === 'ability' ? o.Target : plannedDest(u.ID) || u.Pos;
		fxAt(at, 'ping ' + (o.Action === 'attack' ? 'attack' : o.Action === 'move' ? 'move' : 'ability'), sqSize());
		// A commander with an order left stays in hand (move, then attack
		// from there); anything else is put down.
		if (ordersOf(u.ID).length < limit(u) && o.Action === 'move') { select(u); } else { select(null, true); S.look = u.ID; render(); }
		return true;
	}
	function undo() {
		var u = unit(S.sel), last = (u && mine(u) && ordersOf(u.ID).length ? ordersOf(u.ID) : S.orders).slice(-1)[0];
		var lu = last && unit(last.UnitID);
		if (lu) { fxAt(plannedDest(lu.ID) || lu.Pos, 'ping undo', sqSize()); }
		if (u && mine(u) && ordersOf(u.ID).length) {
			S.orders = S.orders.filter(function (o) { return o.UnitID !== u.ID; });
		} else {
			S.orders.pop();
		}
		select(u && mine(u) ? u : null);
	}

	function abilitiesOf(u) {
		var out = [];
		if (u.IsCommander) {
			var h = C.Heroes[u.Kind];
			['q', 'w', 'e', 'r'].forEach(function (k) { if (h.Abilities[k]) { out.push({ key: k, id: h.Abilities[k] }); } });
		} else {
			var d = C.Units[u.Kind];
			((d && d.Abilities) || []).forEach(function (id, i) { out.push({ key: String(i + 1), id: id }); });
		}
		return out;
	}
	function openAbilities() {
		var u = unit(S.sel);
		if (!u) { return; }
		var sh = $('sheet');
		sh.textContent = '';
		abilitiesOf(u).forEach(function (a) {
			var ab = C.Abilities[a.id] || { Name: a.id };
			var why = '';
			var need = u.IsCommander ? (C.Rules.Unlock[a.key] || 1) : 0;
			if (u.IsCommander && u.Level < need) { why = 'unlocks at level ' + need; }
			else if (u.Cooldowns && u.Cooldowns[a.id] > 0) { why = 'ready in ' + u.Cooldowns[a.id]; }
			var b = document.createElement('button');
			b.innerHTML = '<b>' + ab.Name + '</b><span>' + (why || describe(ab) + ' ' + cap(abilityMeta(ab)) + '.') + '</span>';
			b.disabled = !!why;
			b.onclick = function () { sh.classList.add('hidden'); beginAbility(u, a.id); };
			sh.appendChild(b);
		});
		var close = document.createElement('button');
		close.className = 'close';
		close.textContent = 'close';
		close.onclick = function () { sh.classList.add('hidden'); };
		sh.appendChild(close);
		sh.classList.remove('hidden');
	}
	function beginAbility(u, id) {
		var ts;
		try { ts = call('abilityTargets', u.ID, id, JSON.stringify(S.orders)) || []; } catch (e) { toast(e.message); return; }
		var ab = C.Abilities[id];
		if (ab.Target === 'self' || ab.Target === 'all') {
			if (!ts.length) { toast(ab.Name + ': cannot be cast now'); return; }
			addOrder({ UnitID: u.ID, Action: 'ability', Ability: id, TargetU: u.ID, Target: u.Pos });
			return;
		}
		if (!ts.length) { toast(ab.Name + ': no target in range'); return; }
		S.abil = id;
		S.abilTargets = ts;
		S.moves = [];
		S.targets = [];
		toast(ab.Name + ': tap a target');
		render();
	}

	// tapSquare is what a tap (or a drop) on board square p means.
	function tapSquare(p) {
		if (S.busy) { return; }
		if (S.replay) { var lu = unitAt(p); S.look = lu ? lu.ID : 0; render(); return; }
		var u = unitAt(p);
		if (S.abil) {
			var t = S.abilTargets.filter(function (x) { return same(x.at, p); })[0];
			if (t) {
				addOrder({ UnitID: S.sel, Action: 'ability', Ability: S.abil, TargetU: t.unit || 0, Target: p });
			} else {
				S.abil = null;
				toast('');
				select(unit(S.sel));
			}
			return;
		}
		if (u && mine(u)) {
			if (u.ID === S.sel) { select(null, true); S.look = u.ID; render(); } else { select(u); }
			return;
		}
		var sel = unit(S.sel);
		if (sel) {
			var tgt = S.targets.filter(function (x) { return same(x.at, p); })[0];
			if (tgt) { addOrder({ UnitID: sel.ID, Action: 'attack', TargetU: tgt.unit, Target: p }); return; }
			var mv = S.moves.filter(function (x) { return same(x.to, p); })[0];
			if (mv) { addOrder({ UnitID: sel.ID, Action: 'move', Path: mv.path }); return; }
		}
		// Anywhere else: put down what is in hand, and show what was tapped
		// (an ally's unit is shown, never taken in hand).
		select(null, true);
		S.look = u ? u.ID : 0;
		if (u && tint(u) === 2) { toast('an ally: ' + ownerName(u) + ' gives its orders'); }
		render();
	}

	// ---- touch and mouse: tap, or drag a piece to a square ---------------
	// Under the camera a drag that starts where there is nothing to order
	// pans the board, and two fingers always pan (and pinch between the
	// close view and the whole board).
	var drag = null, fingers = {}, pinch = null;
	// squareAt is the square under a pointer: #cam's box on screen is the
	// board as drawn, panned, zoomed or mid-glide.
	function squareAt(ev) {
		var r = $('cam').getBoundingClientRect();
		var c = Math.floor((ev.clientX - r.left) / (r.width / cols()));
		var rr = Math.floor((ev.clientY - r.top) / (r.height / rows()));
		if (c < 0 || rr < 0 || c >= cols() || rr >= rows()) { return null; }
		return toBoard(c, rr);
	}
	// orderable is whether a drag from p could give an order: your unit (to
	// drag it), or one of the selected unit's move dots, target rings or
	// ability targets. A drag from anywhere else pans.
	function orderable(p) {
		var at = function (x) { return same(x.at || x.to, p); };
		if (S.abil) { return S.abilTargets.some(at); }
		var u = unitAt(p);
		return !S.replay && (!!(u && mine(u)) || S.moves.some(at) || S.targets.some(at));
	}
	function spread() {
		var ps = Object.keys(fingers).map(function (k) { return fingers[k]; }), a = ps[0], b = ps[1] || a;
		return { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2, d: Math.max(1, Math.sqrt((a.x - b.x) * (a.x - b.x) + (a.y - b.y) * (a.y - b.y))) };
	}
	$('boardwrap').addEventListener('pointerdown', function (ev) {
		if (!S) { return; }
		if (S.busy) { if (S.skip) { S.skip(); } return; } // a tap during playback skips it
		if (CAM.on) {
			fingers[ev.pointerId] = { x: ev.clientX, y: ev.clientY };
			if (Object.keys(fingers).length > 1) {
				// a second finger: whatever the first began is off
				ev.preventDefault();
				$('boardwrap').setPointerCapture(ev.pointerId);
				if (drag && drag.el) { render(); }
				drag = null;
				hover(null);
				if (!pinch) { pinch = spread(); pinch.d0 = pinch.d; }
				return;
			}
		}
		var p = squareAt(ev);
		if (!p) { return; }
		ev.preventDefault();
		$('boardwrap').setPointerCapture(ev.pointerId);
		drag = { p: p, x: ev.clientX, y: ev.clientY, moved: false, el: null, over: null,
			pan: CAM.on && CAM.close && !orderable(p), cx: CAM.x, cy: CAM.y, vx: 0, vy: 0, t: Date.now() };
	});
	$('boardwrap').addEventListener('pointermove', function (ev) {
		if (fingers[ev.pointerId]) { fingers[ev.pointerId] = { x: ev.clientX, y: ev.clientY }; }
		if (pinch) {
			var sp = spread();
			if (CAM.close) { CAM.x -= sp.x - pinch.x; CAM.y -= sp.y - pinch.y; camApply(0); }
			pinch.x = sp.x;
			pinch.y = sp.y;
			if (!pinch.done && (sp.d / pinch.d0 < 0.75 || sp.d / pinch.d0 > 1.33)) { pinch.done = true; zoom(sp.d > pinch.d0, sp); }
			return;
		}
		if (!drag) { return; }
		var dx = ev.clientX - drag.x, dy = ev.clientY - drag.y;
		if (drag.pan) {
			if (!drag.moved && Math.abs(dx) + Math.abs(dy) > 8) { drag.moved = true; }
			if (!drag.moved) { return; }
			// the finger's speed, smoothed, for the glide on release
			var now = Date.now(), dt = Math.max(1, now - drag.t);
			drag.vx = 0.7 * (ev.clientX - (drag.lx || drag.x)) / dt + 0.3 * drag.vx;
			drag.vy = 0.7 * (ev.clientY - (drag.ly || drag.y)) / dt + 0.3 * drag.vy;
			drag.lx = ev.clientX;
			drag.ly = ev.clientY;
			drag.t = now;
			CAM.x = drag.cx - dx;
			CAM.y = drag.cy - dy;
			camApply(0);
			return;
		}
		if (!drag.moved && Math.abs(dx) + Math.abs(dy) > 8) {
			var u = unitAt(drag.p);
			if (!u || !mine(u) || S.abil) { return; }
			drag.moved = true;
			if (S.sel !== u.ID) { select(u); }
			drag.el = $('pieces').querySelector('.piece[data-id="' + u.ID + '"]:not(.ghost)');
			if (drag.el) {
				var ds = toScreen(drag.p), dz = sqSize();
				drag.el.classList.add('drag');
				drag.ox = ds.c * dz;
				drag.oy = ds.r * dz;
				drag.s = CAM.on ? camView().s : 1; // screen pixels to board pixels, in the whole view
			}
		}
		if (drag.moved && drag.el) {
			drag.el.style.transform = 'translate3d(' + (drag.ox + dx / drag.s) + 'px,' + (drag.oy + dy / drag.s) + 'px,0)';
			var over = squareAt(ev);
			hover(over);
			drag.over = over;
		}
	});
	function hover(p) {
		Array.prototype.slice.call($('board').querySelectorAll('.hover')).forEach(function (e) { e.classList.remove('hover'); });
		if (!p) { return; }
		var s = toScreen(p);
		var el = $('board').children[s.r * cols() + s.c];
		if (el) { el.classList.add('hover'); }
	}
	// lift ends a finger's part in a gesture; true while a two-finger one
	// is still on (it never ends in a tap).
	function lift(ev) {
		delete fingers[ev.pointerId];
		if (!pinch) { return false; }
		if (!Object.keys(fingers).length) { pinch = null; }
		return true;
	}
	function endDrag(ev) {
		if (lift(ev) || !drag) { return; }
		var d = drag;
		drag = null;
		hover(null);
		if (d.pan) {
			if (!d.moved) { tapSquare(d.p); return; }
			// let go moving: the board glides on a little, unless the finger
			// had stopped or motion is off
			if (Date.now() - d.t < 80 && Math.abs(d.vx) + Math.abs(d.vy) > 0.3 && !still()) {
				CAM.x -= d.vx * 140;
				CAM.y -= d.vy * 140;
				camApply(360);
			}
			return;
		}
		var p = squareAt(ev) || d.p;
		if (d.moved) {
			if (same(p, d.p)) { render(); return; } // dropped where it was
			tapSquare(p);
			if (d.el && d.el.parentNode) { render(); }
			return;
		}
		tapSquare(d.p);
	}
	$('boardwrap').addEventListener('pointerup', endDrag);
	$('boardwrap').addEventListener('pointercancel', function (ev) { lift(ev); drag = null; hover(null); if (S) { render(); } });
	// The zoom button, and the minimap: a tap (or a drag along it) moves the
	// close view there. Neither starts a gesture on the board below.
	$('zoom').addEventListener('pointerdown', function (ev) { ev.stopPropagation(); });
	$('zoom').onclick = function () { zoom(!CAM.close); };
	function miniGo(ev) {
		var r = $('mini').firstChild.getBoundingClientRect(), z = sqSize();
		CAM.x = (ev.clientX - r.left) / r.width * cols() * z - BW / 2;
		CAM.y = (ev.clientY - r.top) / r.height * rows() * z - CAM.vh / 2;
		camApply(ev.type === 'pointerdown' ? 260 : 0);
	}
	$('mini').addEventListener('pointerdown', function (ev) {
		ev.stopPropagation();
		ev.preventDefault();
		$('mini').setPointerCapture(ev.pointerId);
		$('mini').held = true;
		miniGo(ev);
	});
	$('mini').addEventListener('pointermove', function (ev) { if ($('mini').held) { miniGo(ev); } });
	$('mini').addEventListener('pointerup', function () { $('mini').held = false; });
	$('mini').addEventListener('pointercancel', function () { $('mini').held = false; });

	// ---- resolving the turn ----------------------------------------------
	function endTurn() {
		if (S.busy) { return; }
		if (S.online) {
			// Online the server resolves the turn: send the orders (they are the
			// commit) and wait for the other side.
			if (S.online.committed) { return; }
			if (!nsend('orders', { match: S.online.match, turn: S.view.Match.Turn, orders: S.orders })) { toast('not connected'); return; }
			S.online.committed = true;
			S.sel = 0;
			S.moves = [];
			S.targets = [];
			$('sheet').classList.add('hidden');
			render();
			sweep();
			toast(S.online.async ? 'orders in · the menu takes you back while they think' : 'orders in · waiting for the other side');
			return;
		}
		var turn;
		try { turn = call('commit', JSON.stringify(S.orders)); } catch (e) { toast(e.message); return; }
		if (turn.pass) { // pass and play: the first player is done; hand over
			$('sheet').classList.add('hidden');
			S.orders = [];
			handOver(1, turn.post);
			return;
		}
		S.busy = true;
		S.whole = !!turn.whole;
		if (S.whole) { S.you = 0; S.team = (turn.pre.Players[0] || { Team: 0 }).Team; layout(); }
		$('sheet').classList.add('hidden');
		S.sel = 0;
		S.moves = [];
		S.targets = [];
		S.abil = null;
		S.orders = [];
		S.view = turn.pre;
		S.log = logLines(turn.events || [], turn.pre);
		// Last turn's moves stay lit, from and to, as a chess board shows the
		// last move; last turn's deaths leave a scorch mark.
		S.last = {};
		S.scars = {};
		(turn.events || []).forEach(function (e) {
			if (e.k !== 'Moved') { return; }
			var to = e.path && e.path.length ? e.path[e.path.length - 1] : e.to;
			S.last[e.from.X + ',' + e.from.Y] = true;
			S.last[to.X + ',' + to.Y] = true;
		});
		render();
		sweep();
		S.post = turn.post;
		play(turn.events || [], function () {
			S.view = turn.post;
			S.busy = false;
			S.look = 0;
			render();
			if (S.hotseat) {
				S.whole = false;
				if (turn.ended) { over(turn); } else { setTimeout(function () { handOver(0, call('view')); }, 700); }
				return;
			}
			if (turn.ended) { forget(); over(turn); } else { saveGame(); }
		});
	}
	// sweep is the orders going out: a line of light up the board from your side.
	function sweep() {
		var d = document.createElement('div');
		d.className = 'sweep';
		$('fx').appendChild(d);
		setTimeout(function () { d.remove(); }, 700);
	}
	// paceFor keeps a turn near PLAY_MS at normal speed: a long one plays
	// faster, down to a limit that still reads. COST is what animate waits.
	var PLAY_MS = 5500;
	var COST = { Teleported: 420, Pushed: 260, Attacked: 300, Damaged: 320, Healed: 320, Died: 480, Expired: 480, Spawned: 380, Respawned: 380,
		AbilityCast: 520, Telegraph: 300, AbilityResolved: 420, Revealed: 560, Smoke: 420, Status: 300, Overwatch: 200, ObjectiveScored: 380 };
	function paceFor(events) {
		var t = 250;
		events.forEach(function (e) { t += e.k === 'Moved' ? ((e.path && e.path.length) || 1) * 140 + 60 : (COST[e.k] || 0); });
		return Math.min(1, Math.max(0.55, PLAY_MS / t));
	}
	// FXT and FXA are the running turn's timers and animations, so a skip can
	// stop them all. A turn that plays to its end leaves its last effects to
	// finish by themselves.
	var FXT = [], FXA = [];
	// HIT is the way the last blow on each unit came (screen x, y), so the
	// hit knocks it back that way.
	var HIT = {};
	function later(f, ms) {
		var t = setTimeout(function () { var i = FXT.indexOf(t); if (i >= 0) { FXT.splice(i, 1); } f(); }, ms);
		FXT.push(t);
		return t;
	}
	function cutShort() {
		FXT.forEach(clearTimeout);
		FXT = [];
		FXA.forEach(function (a) { try { a.cancel(); } catch (e) { /* already gone */ } });
		FXA = [];
		Array.prototype.slice.call($('fx').children).forEach(function (m) { if (!m.classList.contains('mark')) { m.remove(); } });
		$('hud').textContent = '';
		camApply(0); // a glide under way lands at once
	}
	// play animates the turn's events one after another, then calls done.
	// A tap on the board skips to the end; at speed "instant" nothing plays.
	function play(events, done) {
		var i = 0, finished = false, timer = 0;
		FXT = [];
		FXA = [];
		HIT = {};
		PACE = paceFor(events);
		var finish = function () {
			if (finished) { return; }
			finished = true;
			clearTimeout(timer);
			S.skip = null;
			PACE = 1;
			done();
		};
		S.skip = function () { cutShort(); finish(); };
		if (K() === 0) { finish(); return; }
		var hints = parseInt(stored('rfog.skiphint') || '0', 10);
		if (hints < 3 && events.length > 4) { toast('tap the board to skip'); keep('rfog.skiphint', String(hints + 1)); }
		// Under the camera each event is in view before it plays: the glide
		// to the next one takes the tail of this one's time, never more.
		var glide = 300 * K();
		var step = function () {
			if (finished) { return; }
			if (i >= events.length) { timer = setTimeout(finish, 250 * K()); return; }
			follow(events, i, glide);
			var e = events[i++];
			var wait = 0;
			try { wait = animate(e); } catch (err) { wait = 0; }
			if (CAM.on && i < events.length) {
				var at = i;
				later(function () { if (!finished) { follow(events, at, Math.min(glide, wait * K())); } }, Math.max(0, wait * K() - glide));
			}
			timer = setTimeout(step, wait * K());
		};
		step();
	}
	function pieceOf(id) { return $('pieces').querySelector('.piece[data-id="' + id + '"]:not(.ghost)'); }
	// arrive puts a unit on the board mid-playback: it walked out of the fog
	// or was just spawned, so the turn's first view had no piece for it. It
	// is drawn from the view after the turn; a unit not in that view (seen
	// only in passing) stays undrawn.
	function arrive(id, at) {
		if (unit(id)) { return unit(id); }
		var found = ((S.post && S.post.Units) || []).filter(function (u) { return u.ID === id && !u.Dead && u.HP > 0; })[0];
		if (!found) { return null; }
		var u = JSON.parse(JSON.stringify(found));
		u.Pos = at;
		S.view.Units.push(u);
		var el = pieceEl(u);
		el.style.opacity = '0';
		$('pieces').appendChild(el);
		void el.offsetWidth;
		el.style.opacity = '';
		return u;
	}
	// float pops a number or a word over a square; a little off centre, so
	// two on one unit in a turn do not cover each other.
	function float(p, text, cls) {
		var c = center(p);
		var f = document.createElement('div');
		f.className = 'float ' + cls;
		f.textContent = text;
		f.style.left = (c.x + (Math.random() - 0.5) * sqSize() * 0.24) + 'px';
		f.style.top = c.y + 'px';
		$('fx').appendChild(f);
		later(function () { f.remove(); }, 1300);
	}
	function flash(p, color) {
		var s = toScreen(p), z = sqSize();
		var f = document.createElement('div');
		f.className = 'flash';
		f.style.left = (s.c * z) + 'px';
		f.style.top = (s.r * z) + 'px';
		f.style.width = f.style.height = z + 'px';
		f.style.background = color;
		$('fx').appendChild(f);
		later(function () { f.remove(); }, 420);
	}
	// animate plays one event and says how long to wait before the next.
	// ---- motion helpers. Everything here is cosmetic: the state after the
	// turn is drawn from the engine's result whatever these do.
	// An effect (anything in #fx or #hud) holds its last frame and is removed when it
	// ends, so it never snaps back to where it started.
	function anim(el, frames, ms, easing, iterations) {
		if (!el || !el.animate || K() === 0) { return null; }
		var effect = el.closest && el.closest('#fx, #hud');
		try {
			var a = el.animate(frames, { duration: ms * K(), easing: easing || 'ease-out', iterations: iterations || 1, fill: effect ? 'both' : 'auto' });
			FXA.push(a);
			if (effect) { a.onfinish = function () { el.remove(); }; }
			return a;
		} catch (e) { return null; }
	}
	function bodyOf(id) { var el = pieceOf(id); return el && el.querySelector('.body'); }
	// fxAt puts a short-lived effect element at a square's centre.
	function fxAt(p, cls, size) {
		var c = center(p), d = document.createElement('div');
		d.className = cls;
		d.style.left = c.x + 'px';
		d.style.top = c.y + 'px';
		if (size) { d.style.width = d.style.height = size + 'px'; }
		$('fx').appendChild(d);
		later(function () { d.remove(); }, 1400 * Math.max(K(), 0.3));
		return d;
	}
	// burst throws pixels out of a square (a death, a heal when up is true).
	function burst(p, color, n, up) {
		var z = sqSize();
		for (var i = 0; i < n; i++) {
			var d = fxAt(p, 'px');
			d.style.background = color;
			var a = up ? (-Math.PI / 2 + (Math.random() - 0.5) * 1.4) : Math.random() * Math.PI * 2;
			var r = z * (up ? 0.5 + Math.random() * 0.3 : 0.35 + Math.random() * 0.4);
			anim(d, [{ transform: 'translate(-50%,-50%)', opacity: 1 },
				{ transform: 'translate(calc(-50% + ' + Math.cos(a) * r + 'px), calc(-50% + ' + Math.sin(a) * r + 'px))', opacity: 0 }],
				up ? 700 : 520);
		}
	}
	function teamColor(u) { return u && rel(u.Team) === 1 ? '#ff7043' : '#4fd3e8'; }
	// alpha turns #rrggbb into rgba with the given opacity.
	function alpha(hex, a) {
		var v = parseInt(hex.slice(1), 16);
		return 'rgba(' + (v >> 16 & 255) + ',' + (v >> 8 & 255) + ',' + (v & 255) + ',' + a + ')';
	}
	// kinds is what an ability does, from its effects in the rules.
	function kinds(ab) {
		var k = {};
		(ab.Effects || []).forEach(function (e) {
			k[e.Kind] = true;
			if (e.Kind === 'stat') { k[e.Delta < 0 ? 'stat_down' : 'stat_up'] = true; }
		});
		return k;
	}
	// squareArea is the engine's area: every square within r in both axes.
	function squareArea(c, r) {
		var out = [];
		for (var y = c.Y - r; y <= c.Y + r; y++) {
			for (var x = c.X - r; x <= c.X + r; x++) {
				if (x >= 0 && y >= 0 && x < W() && y < H()) { out.push({ X: x, Y: y }); }
			}
		}
		return out;
	}
	// strike shows a figure's attack pose for a moment.
	function strike(u, ms) {
		var el = pieceOf(u.ID), sp = el && el.querySelector('.body .sprite');
		if (!sp || K() === 0 || !pose(sp, true)) { return; }
		later(function () { pose(sp, false); }, ms * K());
	}
	// ring is a circle of light growing out of a square, size in squares.
	function ring(p, color, size, ms) {
		var d = fxAt(p, 'pulse', sqSize() * size);
		d.style.borderColor = color;
		anim(d, [{ transform: 'translate(-50%,-50%) scale(0.3)', opacity: 1 }, { transform: 'translate(-50%,-50%) scale(1.2)', opacity: 0 }], ms);
	}
	// castRing is a cast going out: a ring whose light falls off inward in
	// dither steps, growing smoothly and dimming a step at a time (three
	// frames of one canvas, drawn once a colour and size, swapped by a
	// stepped transform).
	function castRing(p, color, size, ms) {
		var z = sqSize(), cell = Math.max(2, Math.round(z * 1.12 / ART.size)), n = Math.max(8, Math.round(z * size / cell));
		var src = pool('ring|' + color + '|' + n + '|' + cell, n, n, cell, color, function (r) { return r > 1 ? 0 : r > 0.86 ? 16 : step(1 - r, 0.24, 0.4, 0.56); }, 3);
		var d = fxAt(p, 'castring', n * cell), cv = document.createElement('canvas');
		cv.width = src.width;
		cv.height = src.height;
		cv.getContext('2d').drawImage(src, 0, 0);
		cv.className = 'px';
		cv.style.width = src.style.width;
		cv.style.height = src.style.height;
		d.appendChild(cv);
		anim(d, [{ transform: 'translate(-50%,-50%) scale(0.3)' }, { transform: 'translate(-50%,-50%) scale(1.2)' }], ms);
		anim(cv, [{ transform: 'translateY(0)' }, { transform: 'translateY(-100%)' }], ms, 'steps(3, end)');
	}
	// lob throws an orb in an arc from one square to another.
	function lob(from, to, color) {
		var a = center(from), b = center(to), z = sqSize();
		var d = fxAt(from, 'orb', z * 0.3);
		d.style.background = color;
		d.style.boxShadow = '0 0 10px ' + color;
		var lift = -z * (0.8 + 0.15 * Math.sqrt((b.x - a.x) * (b.x - a.x) + (b.y - a.y) * (b.y - a.y)) / z);
		var at = function (t) { return 'translate(calc(-50% + ' + (b.x - a.x) * t + 'px), calc(-50% + ' + ((b.y - a.y) * t + lift * 4 * t * (1 - t)) + 'px))'; };
		anim(d, [0, 0.25, 0.5, 0.75, 1].map(function (t) { return { transform: at(t), offset: t }; }), 420, 'linear');
		later(function () { d.remove(); impact(to, color, true); }, 420 * Math.max(K(), 0.01));
	}
	// blast hits every square of an area at once.
	function blast(area, color) {
		area.forEach(function (p) {
			flash(p, alpha(color, 0.5));
			burst(p, color, 3);
		});
		if (area.length) {
			ring(area[Math.floor(area.length / 2)], color, Math.sqrt(area.length) * 1.2, 520);
			impact(area[Math.floor(area.length / 2)], color, true);
		}
	}
	// still is whether the player asked for less motion: the setting, or
	// the device's. Only what moves the whole board or screen asks.
	function still() { return PREF.motion === 'off' || !!(window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches); }
	// callout says a moment of the match across the board, in the colour of
	// the side it favours: a commander down, an ultimate, the lead changing
	// hands, match point. One at a time; it goes with the turn's effects.
	var CALL = null;
	function callout(text, cls, sub) {
		if (K() === 0) { return; }
		if (CALL) { CALL.remove(); }
		var d = document.createElement('div'), b = document.createElement('b');
		d.className = 'callout ' + cls;
		if (sub) { var s = document.createElement('small'); s.textContent = sub; d.appendChild(s); }
		b.textContent = text;
		d.appendChild(b);
		$('hud').appendChild(d); // the room's, not the board's: always in view
		CALL = d;
		anim(d, [{ transform: 'translateY(-50%) scaleY(0.2)', opacity: 0 }, { transform: 'translateY(-50%) scaleY(1)', opacity: 1, offset: 0.1 },
			{ transform: 'translateY(-50%) scaleY(1)', opacity: 1, offset: 0.78 }, { transform: 'translateY(-50%) scaleY(1)', opacity: 0 }], 1400);
		anim(b, [{ transform: 'scale(1.6)' }, { transform: 'scale(1)', offset: 0.14 }, { transform: 'scale(1)' }], 1400);
	}
	// who names a side as seen from here: you, or the other side by name; a
	// team as your team, the bots or their team.
	function who(r) {
		var named = S.replay || S.hotseat, n = foes().length;
		if ((r === 0 ? S.view.Players.length - n : n) > 1) { return r === 0 ? 'your team' : named || S.online ? 'their team' : 'the bots'; }
		if (r === 0) { return named ? me().Name : 'you'; }
		return named || S.online ? them().Name || 'they' : 'the bot';
	}
	// crackle is static over a square (slowed, silenced, a field).
	function crackle(p, color) {
		for (var i = 0; i < 3; i++) {
			(function (i) {
				later(function () {
					var z = sqSize(), sk = fxAt(p, 'spark');
					sk.style.background = color;
					sk.style.color = color;
					sk.style.width = (z * (0.2 + Math.random() * 0.3)) + 'px';
					sk.style.marginLeft = ((Math.random() - 0.5) * z * 0.6) + 'px';
					sk.style.marginTop = ((Math.random() - 0.5) * z * 0.6) + 'px';
					sk.style.transform = 'translate(-50%,-50%) rotate(' + Math.round(Math.random() * 180) + 'deg)';
					anim(sk, [{ opacity: 1 }, { opacity: 0 }], 220);
				}, i * 70 * K());
			})(i);
		}
	}
	// reticle is crosshairs closing on a square.
	function reticle(p, color, ms) {
		var d = fxAt(p, 'reticle', sqSize() * 0.9);
		d.style.borderColor = color;
		d.style.color = color;
		anim(d, [{ transform: 'translate(-50%,-50%) scale(1.5) rotate(45deg)', opacity: 0 },
			{ transform: 'translate(-50%,-50%) scale(1) rotate(0deg)', opacity: 1, offset: 0.4 },
			{ transform: 'translate(-50%,-50%) scale(1) rotate(0deg)', opacity: 0 }], ms);
	}
	// chain is a ring of links pulled tight around a rooted unit.
	function chain(p) {
		var d = fxAt(p, 'chain', sqSize() * 0.95);
		anim(d, [{ transform: 'translate(-50%,-50%) scale(1.4) rotate(0deg)', opacity: 0 },
			{ transform: 'translate(-50%,-50%) scale(0.85) rotate(90deg)', opacity: 1, offset: 0.5 },
			{ transform: 'translate(-50%,-50%) scale(0.85) rotate(90deg)', opacity: 0 }], 700);
	}
	// chevrons rise (a buff) or sink (a debuff) over a unit.
	function chevrons(p, color, up) {
		for (var i = 0; i < 2; i++) {
			(function (i) {
				later(function () {
					var d = fxAt(p, 'chev');
					d.textContent = up ? '︽' : '︾';
					d.style.color = color;
					var z = sqSize();
					anim(d, [{ transform: 'translate(-50%,-50%) translateY(' + (up ? z * 0.2 : -z * 0.3) + 'px)', opacity: 0 },
						{ transform: 'translate(-50%,-50%)', opacity: 1, offset: 0.4 },
						{ transform: 'translate(-50%,-50%) translateY(' + (up ? -z * 0.35 : z * 0.25) + 'px)', opacity: 0 }], 600);
				}, i * 140 * K());
			})(i);
		}
	}
	// puff is a billow of smoke over a square.
	function puff(p) {
		var d = fxAt(p, 'puffs', sqSize() * 1.1);
		anim(d, [{ transform: 'translate(-50%,-50%) scale(0.3)', opacity: 0.9 }, { transform: 'translate(-50%,-50%) scale(1.15)', opacity: 0 }], 700);
	}
	// converge is pixels gathering into a square (a summon, a return).
	function converge(p, color) {
		var z = sqSize();
		for (var i = 0; i < 10; i++) {
			var d = fxAt(p, 'px');
			d.style.background = i % 3 ? '#0b0e12' : color;
			var a = Math.random() * Math.PI * 2, r = z * (0.45 + Math.random() * 0.3);
			anim(d, [{ transform: 'translate(calc(-50% + ' + Math.cos(a) * r + 'px), calc(-50% + ' + Math.sin(a) * r + 'px))', opacity: 0 },
				{ transform: 'translate(-50%,-50%)', opacity: 1 }], 360, 'ease-in');
		}
	}
	// slash is a melee arc across a square, facing the way the blow came.
	function slash(p, color, ang, big, miss) {
		var z = sqSize(), d = fxAt(p, 'slash', z * (big ? 1.5 : 1.3));
		d.style.borderTopColor = color;
		d.style.filter = 'drop-shadow(0 0 5px ' + color + ')';
		var a = ang * 180 / Math.PI + 90;
		anim(d, [{ transform: 'translate(-50%,-50%) rotate(' + (a - 70) + 'deg) scale(0.7)', opacity: 1 },
			{ transform: 'translate(-50%,-50%) rotate(' + (a + 50) + 'deg) scale(1)', opacity: 0 }], 280);
		impact(p, color, big, miss);
	}
	// impact is a blow landing: a flash of the attacker's colour with a white
	// core, a star of light turning out of it and pixels thrown. Bigger from
	// a commander. A miss only scatters a few pixels.
	function impact(p, color, big, miss) {
		if (miss) { burst(p, color, 3); return; }
		var z = sqSize() * (big ? 1.7 : 1.25), r = Math.round(Math.random() * 40);
		var f = fxAt(p, 'impact', z);
		f.style.setProperty('--c', color);
		anim(f, [{ transform: 'translate(-50%,-50%) scale(0.2)', opacity: 1 }, { transform: 'translate(-50%,-50%) scale(1)', opacity: 0.9, offset: 0.3 },
			{ transform: 'translate(-50%,-50%) scale(1.2)', opacity: 0 }], 300);
		var s = fxAt(p, 'spikes', z * 1.2);
		s.style.setProperty('--c', color);
		anim(s, [{ transform: 'translate(-50%,-50%) rotate(' + r + 'deg) scale(0.3)', opacity: 1 },
			{ transform: 'translate(-50%,-50%) rotate(' + (r + 30) + 'deg) scale(1.1)', opacity: 0 }], 340);
		burst(p, color, big ? 7 : 5);
	}
	// beam is a ranged shot: a muzzle flash, a bright bolt flying to the
	// target with its streak drawn behind it, the impact where it lands.
	function beam(from, to, color, big, miss) {
		var a = center(from), b = center(to), z = sqSize();
		var len = Math.sqrt((b.x - a.x) * (b.x - a.x) + (b.y - a.y) * (b.y - a.y));
		var d = fxAt(from, 'beam');
		d.style.width = len + 'px';
		d.style.height = Math.max(3, Math.round(z * (big ? 0.09 : 0.06))) + 'px';
		d.style.background = 'linear-gradient(90deg, transparent, ' + color + ' 40%, #fff)';
		d.style.boxShadow = '0 0 8px ' + color;
		var rot = 'rotate(' + Math.atan2(b.y - a.y, b.x - a.x) * 180 / Math.PI + 'deg)';
		anim(d, [{ transform: 'translateY(-50%) ' + rot + ' scaleX(0)', opacity: 1 },
			{ transform: 'translateY(-50%) ' + rot + ' scaleX(1)', opacity: 0.9, offset: 0.5 },
			{ transform: 'translateY(-50%) ' + rot + ' scaleX(1)', opacity: 0 }], 320, 'linear');
		bolt(from, to, color, big);
		var m = fxAt(from, 'muzzle', z * (big ? 0.7 : 0.5));
		m.style.background = 'radial-gradient(closest-side, #fff, ' + color + ' 50%, transparent)';
		anim(m, [{ transform: 'translate(-50%,-50%) scale(1)', opacity: 1 }, { transform: 'translate(-50%,-50%) scale(0.2)', opacity: 0 }], 180);
		later(function () { impact(to, color, big, miss); }, 160 * K());
	}
	// zap is signal thrown along the ground: a jagged line that flickers out.
	function zap(from, to, color, big, miss) {
		var a = center(from), b = center(to), z = sqSize();
		var svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
		svg.setAttribute('class', 'zap');
		var pts = [], steps = 7;
		for (var i = 0; i <= steps; i++) {
			var t = i / steps, j = (i === 0 || i === steps) ? 0 : (Math.random() - 0.5) * z * 0.45;
			var nx = -(b.y - a.y), ny = b.x - a.x, nl = Math.max(1, Math.sqrt(nx * nx + ny * ny));
			pts.push((a.x + (b.x - a.x) * t + nx / nl * j) + ',' + (a.y + (b.y - a.y) * t + ny / nl * j));
		}
		var pl = document.createElementNS('http://www.w3.org/2000/svg', 'polyline');
		pl.setAttribute('points', pts.join(' '));
		pl.setAttribute('fill', 'none');
		pl.setAttribute('stroke', color);
		pl.setAttribute('stroke-width', Math.max(2, z * 0.05));
		pl.setAttribute('stroke-linejoin', 'bevel');
		svg.appendChild(pl);
		svg.style.filter = 'drop-shadow(0 0 4px ' + color + ')';
		$('fx').appendChild(svg);
		anim(svg, [{ opacity: 1 }, { opacity: 0.3, offset: 0.3 }, { opacity: 1, offset: 0.5 }, { opacity: 0 }], 360);
		later(function () { svg.remove(); }, 400 * Math.max(K(), 0.3));
		later(function () { impact(to, color, big, miss); }, 120 * K());
	}
	// shatter breaks a figure into dark fragments that drop, and a few
	// sparks of its colour that rise.
	function shatter(p, color) {
		var z = sqSize();
		for (var i = 0; i < 12; i++) {
			var spark = i >= 9;
			var d = fxAt(p, 'px');
			d.style.background = spark ? color : (i % 3 ? '#0b0e12' : '#46525f');
			d.style.width = d.style.height = (spark ? 3 : 4 + Math.round(Math.random() * 3)) + 'px';
			var sx = (Math.random() - 0.5) * z * 0.8;
			var sy = spark ? -z * (0.5 + Math.random() * 0.4) : z * (0.15 + Math.random() * 0.25);
			anim(d, [{ transform: 'translate(-50%,-50%)', opacity: 1 },
				{ transform: 'translate(calc(-50% + ' + sx * 0.6 + 'px), calc(-50% + ' + (spark ? sy * 0.5 : -z * 0.15) + 'px))', opacity: 1, offset: 0.35 },
				{ transform: 'translate(calc(-50% + ' + sx + 'px), calc(-50% + ' + sy + 'px))', opacity: 0 }], spark ? 700 : 560, 'ease-in');
		}
	}
	// bolt is the head of a ranged shot, travelling from one square to another.
	function bolt(from, to, color, big) {
		var a = center(from), b = center(to), z = sqSize();
		var d = fxAt(from, 'bolt');
		d.style.width = Math.round(z * (big ? 0.6 : 0.45)) + 'px';
		d.style.height = Math.max(4, Math.round(z * (big ? 0.14 : 0.1))) + 'px';
		d.style.background = 'linear-gradient(90deg, transparent, ' + color + ' 45%, #fff)';
		d.style.boxShadow = '0 0 10px ' + color;
		var ang = Math.atan2(b.y - a.y, b.x - a.x) * 180 / Math.PI;
		anim(d, [{ transform: 'translate(-50%,-50%) rotate(' + ang + 'deg)', opacity: 1 },
			{ transform: 'translate(calc(-50% + ' + (b.x - a.x) + 'px), calc(-50% + ' + (b.y - a.y) + 'px)) rotate(' + ang + 'deg)', opacity: 1, offset: 0.95 },
			{ transform: 'translate(calc(-50% + ' + (b.x - a.x) + 'px), calc(-50% + ' + (b.y - a.y) + 'px)) rotate(' + ang + 'deg)', opacity: 0 }],
			170, 'linear');
	}

	// animate plays one event and says how long (before speed) to wait.
	function animate(e) {
		var z = sqSize();
		var el, b;
		switch (e.k) {
			case 'Moved':
				el = pieceOf(e.u);
				if (!el && arrive(e.u, e.from)) { el = pieceOf(e.u); }
				if (!el) { return 0; }
				b = el.querySelector('.body');
				var path = e.path && e.path.length ? e.path : [e.to];
				var mu = unit(e.u), last = path[path.length - 1];
				// One run across the squares, a hop on each.
				anim(el, [(mu && mu.Pos) || e.from].concat(path).map(function (p) { return { transform: xform(p, z) }; }), path.length * 140, 'linear');
				el.style.transform = xform(last, z);
				anim(b, [{ transform: 'translateY(0)' }, { transform: 'translateY(-16%)' }, { transform: 'translateY(0)' }], 140, 'ease-in-out', path.length);
				if (mu) { mu.Pos = last; }
				return path.length * 140 + 60;
			case 'Teleported':
			case 'Pushed':
				el = pieceOf(e.u || e.tu);
				var pu = unit(e.u || e.tu);
				var pc = teamColor(pu);
				if (e.k === 'Teleported') {
					// a blink: gone in sparks, back in sparks
					if (e.from) { burst(e.from, pc, 8); }
					if (el) { anim(el.querySelector('.body'), [{ opacity: 1, transform: 'scaleY(1)' }, { opacity: 0, transform: 'scaleY(0.1)' }], 160); }
					later(function () {
						if (el) {
							el.style.transform = xform(e.to, z);
							anim(el.querySelector('.body'), [{ opacity: 0, transform: 'scaleY(0.1)' }, { opacity: 1, transform: 'scaleY(1)' }], 200);
						}
						converge(e.to, pc);
					}, 170 * K());
					if (pu) { pu.Pos = e.to; }
					return 420;
				}
				if (el) {
					var shove = el.style.transform;
					el.style.transform = xform(e.to, z);
					anim(el, [{ transform: shove }, { transform: xform(e.to, z) }], 180, 'ease-out');
					ring(e.from || e.to, '#cdd5dc', 0.9, 300);
				}
				if (pu) { pu.Pos = e.to; }
				return 260;
			case 'Attacked':
				var au = unit(e.u), tu = unit(e.tu);
				if (!tu) { return 0; }
				var color = teamColor(au);
				if (au) {
					strike(au, 340);
					// the blow's direction on screen, whichever side is at the bottom
					var sa = toScreen(au.Pos), st = toScreen(tu.Pos), dx = st.c - sa.c, dy = st.r - sa.r;
					var len = Math.max(1, Math.sqrt(dx * dx + dy * dy)), big = !!au.IsCommander;
					if (e.hits) { HIT[tu.ID] = { x: dx / len, y: dy / len }; }
					b = bodyOf(au.ID);
					var hero = au.IsCommander && C.Heroes[au.Kind];
					if (hero && hero.Class === 'operator') {
						// the operator throws signal: lightning along the ground
						anim(b, [{ transform: 'none' }, { transform: 'translateY(-10%)', offset: 0.3 }, { transform: 'none' }], 300);
						zap(au.Pos, tu.Pos, color, big, !e.hits);
					} else if (Math.abs(tu.Pos.X - au.Pos.X) + Math.abs(tu.Pos.Y - au.Pos.Y) <= 1) {
						// melee: lunge into the target, a slash arc across it (a
						// commander's crosses back)
						anim(b, [{ transform: 'none' }, { transform: 'translate(' + dx / len * z * 0.38 + 'px,' + dy / len * z * 0.38 + 'px)', offset: 0.45 }, { transform: 'none' }], 260);
						later(function () { slash(tu.Pos, color, Math.atan2(dy, dx), big, !e.hits); }, 100 * K());
						if (big && e.hits) { later(function () { slash(tu.Pos, color, Math.atan2(dy, dx) + 2.2, false); }, 180 * K()); }
					} else {
						// ranged: a recoil, a bolt across the field, a flash at the muzzle
						anim(b, [{ transform: 'none' }, { transform: 'translate(' + -dx / len * z * 0.1 + 'px,' + -dy / len * z * 0.1 + 'px)', offset: 0.3 }, { transform: 'none' }], 220);
						beam(au.Pos, tu.Pos, color, big, !e.hits);
					}
				}
				if (!e.hits) {
					later(function () {
						float(tu.Pos, 'miss', 'miss');
						anim(bodyOf(tu.ID), [{ transform: 'none' }, { transform: 'translateX(14%)', offset: 0.4 }, { transform: 'none' }], 260);
					}, 150 * K());
				}
				return 300;
			case 'Damaged':
				var du = unit(e.tu);
				if (du) {
					// the number pops, bigger and white-hot for the blow that kills
					float(du.Pos, '-' + (e.n || 0), 'hit' + (du.HP - (e.n || 0) <= 0 ? ' lethal' : ''));
					du.HP = Math.max(0, du.HP - (e.n || 0));
					el = pieceOf(du.ID);
					if (el) {
						b = el.querySelector('.body');
						// knocked back the way the blow came, lit white, then it settles
						var hd = HIT[du.ID];
						anim(b, hd ? [{ transform: 'none', filter: 'brightness(3)' },
							{ transform: 'translate(' + hd.x * z * 0.2 + 'px,' + hd.y * z * 0.2 + 'px) scale(0.92,1.06)', filter: 'brightness(2)', offset: 0.2 },
							{ transform: 'translate(' + -hd.x * z * 0.04 + 'px,' + -hd.y * z * 0.04 + 'px)', offset: 0.6 }, { transform: 'none', filter: 'none' }]
							: [{ transform: 'translateX(0)', filter: 'brightness(3)' }, { transform: 'translateX(-9%)' },
								{ transform: 'translateX(8%)' }, { transform: 'translateX(-5%)' }, { transform: 'translateX(0)', filter: 'none' }], 320);
						setHP(el, du);
					}
				}
				return 320;
			case 'Healed':
				var hu = unit(e.tu || e.u);
				if (hu) {
					float(hu.Pos, '+' + (e.n || 0), 'heal');
					burst(hu.Pos, '#7ed98a', 7, true);
					hu.HP = Math.min(hu.MaxHP, hu.HP + (e.n || 0));
					el = pieceOf(hu.ID);
					if (el) { setHP(el, hu); }
				}
				return 320;
			case 'Died':
			case 'Expired':
				var xu = unit(e.u);
				el = pieceOf(e.u);
				if (el) {
					el.dataset.gone = '1'; // never reused, should the unit come back
					b = el.querySelector('.body');
					// the blow lands: the figure holds white for a beat, then breaks
					anim(b, [{ transform: 'none', filter: 'brightness(3)', opacity: 1 }, { transform: 'none', filter: 'brightness(3)', opacity: 1, offset: 0.12 },
						{ transform: 'scale(1.12)', opacity: 1, offset: 0.2 },
						{ transform: 'scale(0.2) rotate(28deg)', filter: 'brightness(0.4)', opacity: 0 }], 460, 'ease-in');
					var hpb = el.querySelector('.hp');
					if (hpb) { hpb.style.opacity = '0'; }
					later(function () { el.style.opacity = '0'; }, 440 * K());
				}
				if (xu) {
					if (e.k === 'Died') {
						shatter(xu.Pos, teamColor(xu));
						// and the board takes it too: a jolt, harder for a commander
						if (xu.IsCommander) { callout(['commander down', 'enemy commander down', 'ally commander down'][tint(xu)], rel(xu.Team) === 1 ? 'own' : 'enemy', heroName(xu.Kind)); }
						if (!still()) {
							var jolt = xu.IsCommander ? 5 : 3;
							anim($('boardwrap'), [{ transform: 'none' }, { transform: 'translate(' + jolt + 'px,' + -jolt * 0.6 + 'px)' },
								{ transform: 'translate(' + -jolt + 'px,' + jolt * 0.6 + 'px)' }, { transform: 'translate(' + jolt * 0.4 + 'px,0)' }, { transform: 'none' }], 240);
						}
						S.scars[xu.Pos.X + ',' + xu.Pos.Y] = true;
						var sq = $('board').children[toScreen(xu.Pos).r * cols() + toScreen(xu.Pos).c];
						if (sq) { sq.appendChild(scorch()); }
					}
					xu.Dead = true;
				}
				return 480;
			case 'Spawned':
			case 'Respawned':
				// pixels gather into a figure
				converge(e.to, rel(e.team || 0) === 1 ? '#ff7043' : '#4fd3e8');
				if (e.k === 'Spawned' && e.u) { later(function () { if (S && S.busy) { arrive(e.u, e.to); } }, 250 * K()); }
				return 380;
			case 'AbilityCast':
				// The caster strikes its pose and the signal goes out: a ring, and
				// an orb lobbed at the target if it is somewhere else.
				var cu = unit(e.u), cab = C.Abilities[e.ab] || {};
				if (cu) {
					var cc = kinds(cab).heal ? '#7ed98a' : teamColor(cu);
					strike(cu, 460);
					castRing(cu.Pos, cc, 0.9, 520);
					// an ultimate is felt across the board: a second, wide ring, the
					// board lit in the caster's colour, its name called out
					if (cu.IsCommander && ((C.Heroes[cu.Kind] || {}).Abilities || {}).r === e.ab) {
						castRing(cu.Pos, cc, 3.2, 720);
						var lit = document.createElement('div');
						lit.className = 'boardlit';
						lit.style.background = 'radial-gradient(circle at ' + center(cu.Pos).x + 'px ' + center(cu.Pos).y + 'px, ' + alpha(cc, 0.45) + ', ' + alpha(cc, 0.12) + ' 60%, transparent)';
						$('fx').appendChild(lit);
						anim(lit, [{ opacity: 0 }, { opacity: 1, offset: 0.2 }, { opacity: 0 }], 700);
						callout(cab.Name || e.ab, rel(cu.Team) === 0 ? 'own' : 'enemy', 'ultimate · ' + heroName(cu.Kind));
					}
					anim(bodyOf(cu.ID), [{ filter: 'brightness(1)' }, { filter: 'brightness(1.8) drop-shadow(0 0 6px ' + cc + ')' }, { filter: 'brightness(1)' }], 520);
					if (e.to && !same(e.to, cu.Pos) && cab.Target !== 'self' && cab.Target !== 'all') {
						lob(cu.Pos, e.to, cc);
						return 620;
					}
				}
				return 420;
			case 'Telegraph':
				// a delayed ability: crosshairs where it will land
				if (e.to) { reticle(e.to, '#ff5a4a', 700); }
				return 300;
			case 'AbilityResolved':
				var rab = C.Abilities[e.ab] || {}, rk = kinds(rab), rc = rel(e.team || 0) === 0 ? '#4fd3e8' : '#ff7043';
				var area = e.to ? squareArea(e.to, rab.Area || 0) : [];
				if (rk.damage) { blast(area, rc); return 520; }
				if (rk.slow || rk.silence || rk.stat_down) { area.forEach(function (p) { crackle(p, rc); }); return 420; }
				return 120;
			case 'Revealed':
				// Ping: a radar sweep out from the centre
				if (e.tiles && e.tiles.length) {
					var rx = 0, ry = 0;
					e.tiles.forEach(function (t) { rx += t.X; ry += t.Y; });
					var mid = { X: Math.round(rx / e.tiles.length), Y: Math.round(ry / e.tiles.length) };
					var span = Math.sqrt(e.tiles.length);
					[0, 1, 2].forEach(function (i) { later(function () { ring(mid, rel(e.team || 0) === 0 ? '#4fd3e8' : '#ff7043', span * 1.1, 700); }, i * 160 * K()); });
				}
				return 560;
			case 'Smoke':
				(e.tiles || []).forEach(function (p, i) { later(function () { puff(p); }, (i % 5) * 40 * K()); });
				return 420;
			case 'Status':
				if (e.why === 'expired') { return 0; }
				var su = unit(e.tu || e.u);
				if (!su) { return 0; }
				var sc = '#cdd5dc';
				switch (e.st) {
					case 'mark': reticle(su.Pos, '#ff5a4a', 600); break;
					case 'root': chain(su.Pos); break;
					case 'slow': crackle(su.Pos, '#9ab8ff'); break;
					case 'silence': ring(su.Pos, '#0b0e12', 1, 500); break;
					case 'shield': ring(su.Pos, '#e8f4f8', 0.95, 600); break;
					case 'cloak': anim(bodyOf(su.ID), [{ opacity: 1 }, { opacity: 0.3 }, { opacity: 1 }], 600); break;
					case 'taunt': reticle(su.Pos, '#e3c05c', 500); break;
					case 'stat': chevrons(su.Pos, (e.n || 0) >= 0 ? '#7ed98a' : '#ff5a4a', (e.n || 0) >= 0); break;
					default: ring(su.Pos, sc, 0.9, 400);
				}
				return 300;
			case 'Overwatch':
				var ou = unit(e.u);
				if (ou && e.n) { ring(ou.Pos, teamColor(ou), 2.2, 700); return 400; }
				return 0;
			case 'LevelUp':
				// A level is seen where it happens, with the key it opens.
				var lu = unit(e.u);
				if (!lu) { return 0; }
				var opened = ['e', 'r'].filter(function (k) { return C.Rules.Unlock[k] === e.n; }).join('').toUpperCase();
				float(lu.Pos, 'L' + e.n + (opened ? ' · ' + opened : ''), 'score ' + (rel(lu.Team) === 0 ? 'own' : 'enemy'));
				ring(lu.Pos, teamColor(lu), 1.6, 620);
				// in a team mode only your own commander's is called out: ten
				// commanders would call it ten times a match
				if (/R/.test(opened) && (mine(lu) || !allied())) { callout('ultimate ready', rel(lu.Team) === 0 ? 'own' : 'enemy', heroName(lu.Kind) + ' · level ' + e.n); }
				return 420;
			case 'ObjectiveScored':
				// Points rise from where they were earned, and the bars fill now.
				var team = e.team || 0, n = e.n || 0, ts = S.view.Teams, win = S.view.Match.WinScore;
				var was = ts[S.team].Score - ts[1 - S.team].Score, before = ts[team].Score;
				ts[team].Score += n;
				renderTop();
				var cls = rel(team) === 0 ? 'own' : 'enemy', lead = ts[S.team].Score - ts[1 - S.team].Score;
				// the side's score jumps, and the turn of the match is said aloud
				anim($(rel(team) === 0 ? 'myscore' : 'oppscore'), [{ transform: 'scale(1)' }, { transform: 'scale(1.45)', offset: 0.3 }, { transform: 'scale(1)' }], 460, 'ease-out');
				var lw = who(lead > 0 ? 0 : 1);
				var took = n && (lead > 0 && was <= 0 || lead < 0 && was >= 0) ? lw + (/^(you|they|the bots)$/.test(lw) ? ' take' : ' takes') + ' the lead' : '';
				if (n && ts[team].Score >= win - 1 && before < win - 1 && ts[team].Score < win) { callout('match point', cls, took || who(rel(team)) + ' · first to ' + win); }
				else if (took) { callout(took, lead > 0 ? 'own' : 'enemy'); }
				if (e.tiles && e.tiles.length) {
					var cx = 0, cy = 0;
					e.tiles.forEach(function (t) { cx += t.X; cy += t.Y; });
					float({ X: Math.round(cx / e.tiles.length), Y: Math.round(cy / e.tiles.length) }, '+' + n, 'score ' + cls);
				} else if (e.why === 'commander') {
					toast(rel(team) === 0 ? 'enemy commander down: +' + n + ' to ' + who(0) : (allied() ? 'a commander on your team fell' : 'your commander fell') + ': +' + n + ' to ' + (S.online ? 'them' : who(1)));
				}
				return 380;
			case 'OrderRejected':
				if (e.u && mine(unit(e.u))) { toast((unit(e.u).Name || 'order') + ': ' + (e.why || 'order refused')); }
				return 0;
		}
		return 0;
	}

	// localGames are matches against bots on this device: your games too,
	// kept here (the server never sees them; never on a leaderboard).
	function localGames() { try { return JSON.parse(stored('rfog.games') || '[]'); } catch (e) { return []; } }
	function recordLocal(res, score) {
		var gs = localGames(), v = S.view;
		var opp = them(), cmd = unit(me().Commander), oc = (v.Units.filter(function (u) { return u.ID === opp.Commander; })[0] || {});
		var at = Date.now();
		gs.unshift({ at: at, res: res, score: score, level: pick.level, hero: cmd ? cmd.Kind : pick.hero, vs: oc.Kind || '', turns: v.Match.Turn, mode: v.Match.Mode, length: lengthOf(v) });
		// its replay too, for the last 20 (each is a few KB)
		try { keep('rfog.replay.' + at, G.gameReplay()); } catch (e) { /* engine or storage gone */ }
		gs.slice(20).forEach(function (g) { keep('rfog.replay.' + g.at, null); });
		keep('rfog.games', JSON.stringify(gs.slice(0, 50)));
	}
	// over is the end of a match on this device. The winner is a team, and
	// yours is the first seat's.
	function over(turn) {
		S.view = turn.post;
		var mt = S.view.Players[0].Team, won = turn.winner === mt, draw = turn.winner < 0, sc = [turn.post.Teams[mt].Score, turn.post.Teams[1 - mt].Score];
		if (S.hotseat) {
			var wp = Math.max(0, turn.winner);
			result({ title: draw ? 'DRAW' : 'PLAYER ' + (wp + 1) + ' WINS', res: draw ? 'draw' : 'won', score: sc, sub: 'player 1 – player 2 · ' + (turn.result || ''),
				hero: commanderOf(wp), team: wp, again: startHotseat });
			return;
		}
		try { recordLocal(draw ? 'draw' : won ? 'won' : 'lost', sc[0] + '–' + sc[1]); } catch (e) { /* storage off */ }
		result({ title: draw ? 'DRAW' : won ? 'SIGNAL HELD' : 'SIGNAL LOST', res: draw ? 'draw' : won ? 'won' : 'lost', score: sc,
			sub: versus() + ' · ' + (turn.result || ''), hero: commanderOf(S.you), again: againSame() });
	}
	// versus is who played a bot match, for the end card: "you – normal bot",
	// "your team – 2 normal bots · 2v2 · long".
	function versus() {
		var nf = foes().length, sm = sameMatch();
		return (nf > 1 ? 'your team – ' + nf + ' ' + pick.level + ' bots' : 'you – ' + pick.level + ' bot') + matchTag(sm.mode, sm.length);
	}
	// againSame is Play again: the same mode and length, the bot card's level
	// and your commander, straight to the bots filling their seats.
	function againSame() { var sm = sameMatch(); return function () { pickBots(sm, true); }; }
	// commanderOf is the kind of a player's commander, for the end card.
	function commanderOf(pid) {
		var p = (S.view.Players || []).filter(function (x) { return x.ID === pid; })[0], u = p && unit(p.Commander);
		return u ? u.Kind : '';
	}
	// result fills the end card and shows it: the commander (striking its
	// pose on a win), the score, a rating counting to its new value, and
	// Play again as the obvious next tap when there is one.
	function result(o) {
		var ov = $('over');
		ov.classList.remove('won', 'lost', 'draw');
		ov.classList.add(o.res);
		$('result').textContent = o.title;
		$('resultscore').innerHTML = o.score ? '<span class="own">' + o.score[0] + '</span><i>–</i><span class="enemy">' + o.score[1] + '</span>' : '';
		$('resultsub').textContent = o.sub || '';
		var fig = $('overfig');
		fig.textContent = '';
		if (o.hero) {
			var sp = document.createElement('div');
			fig.appendChild(sp);
			sprite(sp, 'hero_' + o.hero, 120, o.team || 0, { breath: true });
			if (o.res === 'won') { pose(sp, true); }
			// standing in a pool of its colour, in dither steps of its own pixels
			var pc = TEAM[o.team || 0][0];
			fig.insertBefore(pool('pool|' + pc, 26, 8, 120 / ART.size, pc, function (r) { return step(r, 0.45, 0.75, 1); }), fig.firstChild);
		}
		var rt = $('resultrating'), rd = o.rating;
		rt.textContent = '';
		var ag = $('again');
		ag.classList.toggle('hidden', !o.again);
		ag.textContent = o.label || 'Play again';
		ag.onclick = function () { S = null; showPick(); o.again(); };
		$('tomenu').onclick = function () { S = null; showPick(); if (o.menu) { o.menu(); } };
		setTimeout(function () {
			show('over');
			if (!rd) { return; }
			var from = Math.round(rd.before), to = Math.round(rd.after), t0 = 0;
			var put = function (v) {
				rt.innerHTML = 'rating <b>' + v + '</b> <span class="' + (to < from ? 'down' : 'up') + '">' + (to < from ? '−' : '+') + Math.abs(v - from) + '</span>';
			};
			if (still()) { put(to); return; }
			put(from);
			var step = function (t) {
				t0 = t0 || t;
				var k = Math.min(1, (t - t0) / 900);
				put(Math.round(from + (to - from) * (1 - Math.pow(1 - k, 3))));
				if (k < 1) { requestAnimationFrame(step); }
			};
			setTimeout(function () { requestAnimationFrame(step); }, 350);
		}, o.now ? 0 : 600);
	}

	// ---- the menu: resign, a new game, the terminal version -------------
	$('menu').onclick = function () {
		if (S && S.replay) { closeReplay(); return; }
		if (!S || S.busy) { return; }
		var sh = $('sheet');
		sh.textContent = '';
		var add = function (label, sub, on, cls) {
			var b = document.createElement('button');
			b.innerHTML = '<b>' + label + '</b><span>' + sub + '</span>';
			if (cls) { b.className = cls; }
			b.onclick = on;
			sh.appendChild(b);
			return b;
		};
		if (S.online && S.online.async) { add('Back to menu', 'the match waits for your move', leaveDaily); }
		var resign = add('Resign', 'end this match as a loss', function () {
			if (!resign.dataset.sure) {
				resign.dataset.sure = '1';
				resign.innerHTML = '<b>Tap again to resign</b><span>the other side takes the match</span>';
				return;
			}
			sh.classList.add('hidden');
			if (S.online) { nsend('leave', { match: S.online.match }); toast('leaving the match'); return; }
			if (S.hotseat) {
				result({ title: 'PLAYER ' + (2 - S.you) + ' WINS', res: 'won', sub: 'player ' + (S.you + 1) + ' resigned on turn ' + S.view.Match.Turn,
					hero: commanderOf(1 - S.you), team: 1 - S.you, again: startHotseat, now: true });
				return;
			}
			try { recordLocal('lost', 'resigned'); } catch (e) { /* storage off */ }
			forget();
			result({ title: 'SIGNAL LOST', res: 'lost', sub: 'you resigned on turn ' + S.view.Match.Turn, hero: commanderOf(S.you), again: againSame(), now: true });
		});
		add('How to score', 'objectives, contesting, the lead', rules);
		add('Settings', 'playback, board, motion', settings);
		add('Close', '', function () { sh.classList.add('hidden'); }, 'close');
		sh.classList.remove('hidden');
	};
	// settings shows the preferences in the sheet; each change applies at once.
	function settings() {
		var sh = $('sheet');
		sh.textContent = '';
		var row = function (label, key, values) {
			var r = document.createElement('div');
			r.className = 'setting';
			var l = document.createElement('span'); l.textContent = label; r.appendChild(l);
			var seg = document.createElement('div'); seg.className = 'seg';
			values.forEach(function (v) {
				var b = document.createElement('button');
				if (key === 'board') { // a swatch per board: nine names do not fit a phone
					var c = board(v);
					b.title = v;
					b.setAttribute('aria-label', v);
					b.innerHTML = '<i style="background:linear-gradient(135deg,' + c[1] + ' 50%,' + c[2] + ' 50%)"></i>';
					seg.classList.add('swatchseg');
				} else {
					b.textContent = v;
				}
				if (PREF[key] === v) { b.className = 'on'; }
				b.onclick = function () { PREF[key] = v; applyPrefs(); settings(); if (S) { render(); } };
				seg.appendChild(b);
			});
			r.appendChild(seg);
			sh.appendChild(r);
		};
		row('Playback', 'speed', ['slow', 'normal', 'fast', 'instant']);
		row('Board', 'board', BOARDS.map(function (b) { return b[0]; }));
		row('Ambient motion', 'motion', ['on', 'off']);
		var close = document.createElement('button');
		close.className = 'close';
		close.textContent = 'done';
		close.onclick = function () { sh.classList.add('hidden'); };
		sh.appendChild(close);
		sh.classList.remove('hidden');
	}
	// rules explains scoring from the rules the engine is using.
	function rules() {
		var R = C.Rules, sh = $('sheet');
		sh.textContent = '';
		var d = document.createElement('div');
		d.className = 'rules';
		d.innerHTML =
			'<h3>How to score</h3>' +
			'<p><b>Objectives</b> are the outlined zones. A side <b>holds</b> one when only its units stand in it. ' +
			'One enemy unit anywhere in it, even hidden in the fog, makes it <b>contested</b>: nobody holds it.</p>' +
			'<p>An objective counts <b>once</b>, however many units are in it.</p>' +
			(R.Scoring === 'net'
				? '<p>At the end of each turn only the side holding <b>more</b> objectives scores, by the difference: hold 2 against 1 and you score 1.</p>'
				: '<p>At the end of each turn each side scores 1 per objective it holds.</p>') +
			'<p>Killing a commander gives the other side <b>' + R.CommanderDeathPoints + '</b>. Commanders come back after a few turns.</p>' +
			'<p>First to <b>' + S.view.Match.WinScore + '</b> wins; after ' + S.view.Match.MaxTurns + ' turns the higher score wins.</p>';
		sh.appendChild(d);
		var close = document.createElement('button');
		close.className = 'close';
		close.textContent = 'done';
		close.onclick = function () { sh.classList.add('hidden'); };
		sh.appendChild(close);
		sh.classList.remove('hidden');
	}
	// Planned orders are kept too when the tab goes to the background.
	document.addEventListener('visibilitychange', function () { if (document.hidden && S && !S.busy) { saveGame(); } });

	// ---- online -----------------------------------------------------------
	// The page speaks the game protocol to the server over a WebSocket at
	// /net: the same messages as the terminal client, so an account, its
	// ratings and its matches are the same everywhere. The server resolves
	// every turn; the engine here only previews and checks orders against
	// the fogged view it is sent.
	var NET = { ws: null, state: 'off', welcome: null, err: '', match: null, draft: null, pending: null,
		form: 'login', code: '', ladders: {}, lobby: [], acct: null, retry: 0, mode: 'casual', status: null, local: null,
		// joined: this page is playing the match the server sends (it queued
		// for it, or rejoined it). Until then a match found on sign-in is held
		// in resume and offered in the banner, as a chess app does.
		joined: false, resume: null, last: null, lastEnd: null, displaced: false };
	var TOKEN = 'rfog.token', NAME = 'rfog.name';

	// serverURL is where the server is: the page's own host, or another one
	// given as ?server=host (a static copy of the page elsewhere).
	// anonAuth is an anonymous guest's sign-in: a throwaway name.
	// reconnect signs in again: this device's sign-in, else a new anonymous guest.
	function reconnect() { connect(stored(TOKEN) ? { token: stored(TOKEN), name: stored(NAME) || '' } : anonAuth()); }
	function anonAuth() { return { name: 'guest' + (10000 + Math.floor(Math.random() * 90000)) }; }
	function serverURL() {
		var q = /[?&]server=([^&]+)/.exec(location.search);
		// ?server= (remembered), else the page's own default (set when the
		// page is hosted apart from the game server, e.g. on GitHub Pages),
		// else the host that served the page.
		var meta = document.querySelector('meta[name="rfog-server"]');
		var host = q ? decodeURIComponent(q[1]) : (stored('rfog.server') || (meta && meta.content) || location.host);
		if (q) { keep('rfog.server', host); }
		var secure = location.protocol === 'https:' || /^wss:/.test(host);
		host = host.replace(/^wss?:\/\//, '');
		return (secure ? 'wss://' : 'ws://') + host + '/net';
	}
	function nsend(t, body) {
		if (NET.ws && NET.ws.readyState === 1) { NET.ws.send(JSON.stringify({ v: 1, t: t, b: body || null })); return true; }
		return false;
	}
	// connect signs in: with this device's token, or as asked (guest name,
	// login, new account, recovery).
	function connect(auth) {
		if (NET.ws) { try { NET.ws.onclose = null; NET.ws.close(); } catch (e) { /* gone */ } }
		NET.state = 'connecting';
		NET.err = '';
		renderOnline();
		var ws;
		try { ws = new WebSocket(serverURL()); } catch (e) { NET.state = 'error'; NET.err = 'cannot reach the server'; renderOnline(); return; }
		NET.ws = ws;
		ws.onopen = function () {
			var fp = '';
			try { fp = JSON.parse(G.fingerprint()); } catch (e) { /* old engine */ }
			nsend('hello', { v: 1, client: 'web', rules: fp });
			nsend('auth', auth);
		};
		ws.onmessage = function (ev) {
			var f;
			try { f = JSON.parse(ev.data); } catch (e) { return; }
			try { onFrame(f.t, f.b || {}); } catch (e) { oops(e.message); }
		};
		ws.onclose = function () {
			var was = NET.state;
			NET.ws = null;
			if (NET.displaced) { banner(); return; } // playing elsewhere: do not take it back on our own
			if (was === 'lobby' || was === 'queued' || NET.match) {
				// A dropped connection (a phone switching apps): back in with the
				// token; the server resends the match if one is on.
				NET.state = 'connecting';
				renderOnline();
				if (S && S.online) { toast('reconnecting…'); }
				var wait = Math.min(8000, 800 * Math.pow(2, NET.retry++));
				setTimeout(function () { if (!NET.ws) { reconnect(); } }, wait);
				return;
			}
			if (was === 'connecting') { NET.state = 'error'; NET.err = NET.err || 'cannot reach the server'; }
			renderOnline();
		};
	}
	function onFrame(t, b) {
		// A frame for some other match than the one held (a daily moving on
		// while you play another): Your games picks it up.
		if (/^(draft_state|turn_start|resolved|match_end)$/.test(t) && NET.match && b.match && b.match !== NET.match.match) {
			nsend('live', {});
			return;
		}
		switch (t) {
			case 'challenge_info':
				onChallenge(b);
				return;
			case 'pong':
				if (NET.pingAt) { NET.ping = Date.now() - NET.pingAt; NET.pingAt = 0; }
				return;
			case 'replay_data':
				var rf = NET.replayFor && NET.replayFor.match === b.match ? NET.replayFor : { you: 0, title: 'replay' };
				openReplay(JSON.stringify(b.replay), rf.you < 0 ? 0 : rf.you, rf.title);
				return;
			case 'history_list':
				NET.history = b.matches || [];
				renderHome();
				if (!$('games').classList.contains('hidden')) { renderGames(); }
				return;
			case 'lobby_list':
				NET.lobby = b.matches || [];
				renderHome();
				if (!$('watch').classList.contains('hidden')) { renderWatch(); }
				return;
			case 'spec_state':
				onSpec(b);
				return;
			case 'ladder_list':
				NET.ladders[b.mode] = b.rows || [];
				renderHome();
				if (!$('ladder').classList.contains('hidden')) { renderLadder(); }
				return;
			case 'welcome':
				NET.welcome = b;
				NET.retry = 0;
				NET.displaced = false;
				NET.last = b.last || null;
				NET.live = b.live || [];
				keep(TOKEN, b.token);
				keep(NAME, b.name);
				// Online, the server's rules are the ones that count.
				if (b.content) {
					if (!NET.local) { NET.local = C; }
					call('useContent', JSON.stringify(b.content));
					C = b.content;
				}
				NET.state = 'lobby';
				NET.code = b.recovery || '';
				if (!b.guest) { NET.wantSignin = false; }
				LADDERS.forEach(function (m) { nsend('ladder', { mode: m }); });
				nsend('lobby', {});
				nsend('history', { limit: 8 });
				if (NET.pendingQueue) { var pq = NET.pendingQueue; NET.pendingQueue = null; quickPair(pq); }
				if (NET.invite) { nsend('challenge', { action: 'peek', code: NET.invite }); }
				if (NET.pendingChallenge) { NET.pendingChallenge = false; challengeCard(); }
				renderOnline();
				banner();
				return;
			case 'error':
				if (b.code === 'displaced') {
					// The same player opened the game somewhere else; it continues
					// there. Stay off until asked to take it back.
					NET.displaced = true;
					NET.joined = false;
					if (NET.ws) { try { NET.ws.close(); } catch (e) { /* gone */ } }
					clearInterval(clockTimer);
					banner();
					return;
				}
				if (b.code === 'auth') {
					NET.state = 'signin';
					if (!NET.wantSignin && NET.retry < 4) {
						// A stale sign-in on open, or a guest name already used: carry on
						// as a new anonymous guest.
						keep(TOKEN, null);
						NET.retry++;
						connect(anonAuth());
						return;
					}
					NET.err = b.msg;
					if (/recovery/.test(b.msg)) { NET.form = 'recover'; }
					keep(TOKEN, null);
					renderOnline();
					return;
				}
				if (S && S.online) { toast(b.msg); S.online.committed = /already committed/.test(b.msg); render(); } else {
					if (PK && PK.match && PK.sent) { PK.sent = false; PK.err = b.msg; drawPicks(); }
					NET.err = b.msg;
					if (NET.state === 'queued') { NET.state = 'lobby'; }
					note(b.msg);
					renderOnline();
				}
				return;
			case 'queue_status':
				NET.status = b;
				NET.state = b.mode ? 'queued' : 'lobby';
				renderOnline();
				return;
			case 'live_list':
				NET.live = b.matches || [];
				renderHome();
				return;
			case 'match_found':
				if (b.async && !NET.joined) { nsend('live', {}); return; } // a daily match moved on: Your games shows it
				NET.match = b;
				NET.draft = null;
				if (!NET.joined) { NET.resume = { found: b }; banner(); }
				return;
			case 'draft_state':
				if (!NET.joined && !NET.match) { nsend('live', {}); return; }
				NET.draft = b;
				if (!NET.joined) { if (NET.resume) { NET.resume.turn = null; } banner(); return; }
				showDraft();
				return;
			case 'turn_start':
				if (!NET.joined && !NET.match) { nsend('live', {}); return; }
				if (!NET.joined) { if (NET.resume) { NET.resume.turn = b; } banner(); return; }
				if (S && S.busy) { NET.pending = b; return; } // after the playback
				// the first turn waits for the pick screen to show the full lineup
				if (PK && PK.match === b.match && !PK.started && PK.go && !$('draft').classList.contains('hidden')) { PK.start = b; if (!PK.cut) { PK.go(); } return; }
				turnStart(b);
				return;
			case 'resolved':
				if (!NET.joined) { return; }
				resolvedOnline(b);
				return;
			case 'match_end':
				if (!NET.joined) { NET.lastEnd = { end: b, you: NET.match ? NET.match.you : -1 }; NET.resume = null; NET.match = null; banner(); return; }
				if (S && S.busy) { NET.pendingEnd = b; return; }
				matchEnd(b);
				return;
			case 'account_done':
				accountDone(b);
				return;
		}
	}

	// ---- online: the pick screen's online tab --------------------------------
	function renderOnline() {
		var box = $('modalcard');
		if (!box) { return; }
		renderHome();
		var h = '', w = NET.welcome;
		var field = function (id, label, type, val) {
			return '<label class="fld"><span>' + label + '</span><input id="' + id + '" type="' + (type || 'text') + '" value="' + (val || '').replace(/"/g, '&quot;') + '" autocomplete="off" autocapitalize="off" spellcheck="false"></label>';
		};
		if (NET.code) {
			h = '<div class="card"><b>Your recovery code</b><code class="code">' + NET.code + '</code>' +
				'<p>Write it down. If you forget your password, choose <i>forgot password</i> and use this code to set a new one. ' +
				'There is no email: this code is the way back. It is shown once.</p><button id="codeok" class="primary">Noted</button></div>';
		} else if ((NET.state === 'connecting' || NET.state === 'off') && NET.wantSignin) {
			h = '<p class="sub">connecting…</p>';
		} else if (NET.state === 'error' && NET.wantSignin) {
			h = '<p class="note">' + NET.err + '</p><button id="retry" class="primary">Retry</button>';
		} else if (NET.wantSignin && (NET.state === 'signin' || (w && w.guest))) {
			var f = NET.form === 'guest' ? 'login' : NET.form;
			h = '<h3>' + (f === 'register' ? 'Register' : f === 'recover' ? 'Reset password' : 'Sign in') + '</h3>';
			if (f !== 'recover') {
				h += '<div class="tabs seg"><button data-f="login"' + (f === 'login' ? ' class="on"' : '') + '>Sign in</button>' +
					'<button data-f="register"' + (f === 'register' ? ' class="on"' : '') + '>Register</button></div>';
			}
			var nm = w && !w.guest ? w.name : (/^guest\d+$/.test(stored(NAME) || '') ? '' : stored(NAME) || '');
			if (f === 'login') {
				h += field('fname', 'name', 'text', nm) + field('fpass', 'password', 'password') + '<button id="go" class="primary">Log in</button>' +
					'<button id="forgot" class="link">forgot password</button>';
			} else if (f === 'register') {
				h += field('fname', 'name', 'text', nm) + field('fpass', 'password', 'password') + '<button id="go" class="primary">Register</button>' +
					'<p class="small">a name and a password, nothing else; a recovery code stands in for an email' + (w && w.guest ? '. The games you played here stay yours.' : '') + '</p>';
			} else if (f === 'recover') {
				h += field('fname', 'name', 'text', nm) + field('fcode', 'recovery code') + field('fpass', 'new password', 'password') +
					'<button id="go" class="primary">Set new password</button><button id="back" class="link">back</button>';
			}
			if (NET.err) { h += '<p class="note">' + NET.err + '</p>'; }
		} else if (NET.acct) {
			h = accountHTML();
		} else if (NET.state === 'queued' && !NET.hideSeek) {
			var st = NET.status || {};
			h = '<h3>Seeking a game</h3><p class="sub"><b>' + clockLabel(st.mode || NET.mode) + '</b> · 1v1 · ' + (st.waiting || 0) + ' waiting</p>' +
				'<span class="acquire"><i></i><i></i><i></i><i></i><i></i></span><button id="cancel" class="primary ghost">Cancel</button>' +
				(isDaily(NET.mode) ? '<p class="small">a daily game can take a while to find; close this and keep looking</p>' : '');
		}
		if (!h) { closeModal(); box.innerHTML = ''; return; }
		if (NET.state !== 'queued' || NET.acct || NET.code || isDaily(NET.mode)) { h = CLOSE + h; }
		box.innerHTML = h;
		openModal();
		var mx = box.querySelector('.mclose');
		if (mx) {
			mx.onclick = function () {
				NET.wantSignin = false; NET.acct = null; NET.code = ''; NET.pendingQueue = null; NET.err = '';
				if (NET.state === 'queued') { NET.hideSeek = true; }
				if (!NET.ws && NET.state === 'signin') { connect(anonAuth()); return; } // closed without signing in: carry on anonymous
				renderOnline();
			};
		}
		var on = function (id, fn) { var e = document.getElementById(id); if (e) { e.onclick = fn; } };
		var val = function (id) { var e = document.getElementById(id); return e ? e.value.trim() : ''; };
		on('codeok', function () { NET.code = ''; renderOnline(); });
		on('retry', reconnect);
		each(box, '[data-f]', function (b) { b.onclick = function () { NET.form = b.dataset.f; NET.err = ''; renderOnline(); }; });
		on('forgot', function () { NET.form = 'recover'; NET.err = ''; renderOnline(); });
		on('back', function () { NET.form = 'login'; NET.err = ''; renderOnline(); });
		on('go', function () {
			var n = val('fname');
			if (!n) { NET.err = 'name required'; renderOnline(); return; }
			var a = { name: n };
			if (NET.form === 'login') { a.password = val('fpass'); }
			if (NET.form === 'register') { a.password = val('fpass'); a.register = true; }
			if (NET.form === 'recover') { a.recovery = val('fcode'); a.password = val('fpass'); }
			if (!a.password) { NET.err = 'password required'; renderOnline(); return; }
			// An anonymous guest who registers keeps what it played: the account
			// is made from the guest.
			if (NET.form === 'register' && NET.welcome && NET.welcome.guest && NET.ws) {
				nsend('account', { action: 'save', name: n, password: a.password });
				return;
			}
			keep(NAME, n);
			connect(a);
		});
		each(box, '[data-mode]', function (b) { b.onclick = function () { NET.mode = b.dataset.mode; renderOnline(); }; });
		on('queue', function () { NET.err = ''; NET.joined = true; nsend('queue', { mode: NET.mode, size: '1v1', hero: pick.hero }); NET.state = 'queued'; renderOnline(); });
		on('cancel', function () { nsend('cancel', {}); NET.state = 'lobby'; renderOnline(); });
		on('acct', function () { NET.acct = { form: null }; NET.err = ''; renderOnline(); });
		wireAccount(box);
	}
	// ---- online: the banner ----------------------------------------------------
	// At the top of every screen but a match: a match in progress to rejoin
	// (from this device or another), the game continuing elsewhere, or how
	// the last match went, as a chess app shows when it opens.
	// rejoinResume goes into the match found on sign-in.
	// openDaily goes into a daily match: the server sends it again on join.
	function openDaily(lm) {
		NET.joined = true;
		NET.waitingOn = lm.your_turn ? null : lm.match; // orders already in
		nsend('join', { match: lm.match });
	}
	// leaveDaily goes back to the home screen; the match waits.
	function leaveDaily() {
		NET.joined = false;
		NET.match = null;
		NET.draft = null;
		clearInterval(clockTimer);
		S = null;
		$('sheet').classList.add('hidden');
		showPick();
		nsend('live', {});
	}
	function rejoinResume() {
		var r = NET.resume;
		if (!r) { return; }
		NET.resume = null;
		NET.joined = true;
		NET.match = r.found;
		if (r.turn) { turnStart(r.turn); } else if (NET.draft) { showDraft(); } else { nsend('join', { match: r.found.match }); }
		banner();
	}
	function banner() {
		var el = $('banner');
		if (!el) { return; }
		renderHome(); // its "your games" panel says the same
		var h = '', act = null, dismiss = null;
		// On the home screen "your games" says all this; only the takeover
		// notice shows there.
		var onHome = !$('pick').classList.contains('hidden');
		var inGame = onHome && !NET.displaced || S && S.online && !$('game').classList.contains('hidden');
		if (NET.displaced) {
			h = '<span>This game continued on another device</span><button class="primary">Take it back</button>';
			act = function () { NET.displaced = false; NET.joined = true; reconnect(); banner(); };
		} else if (NET.resume && !inGame) {
			h = '<span><b>Match in progress</b> vs ' + opponents(NET.resume.found || {}) + ' · ' + (NET.resume.found.time || '') + '</span><button class="primary">Rejoin</button>';
			act = rejoinResume;
		} else if (NET.lastEnd && !inGame) {
			var e = NET.lastEnd.end, seat = ((e.final && e.final.Players) || []).filter(function (p) { return p.ID === NET.lastEnd.you; })[0];
			var how = e.winner < 0 ? 'a draw' : seat && seat.Team === e.winner ? 'won' : 'lost';
			h = '<span class="' + resClass(how) + '">Your match ended while you were away: ' + how + (e.result ? ' (' + e.result + ')' : '') + '</span>';
			dismiss = function () { NET.lastEnd = null; banner(); };
		} else if (NET.last && !inGame && stored('rfog.seen') !== NET.last.match) {
			var l = NET.last, res = outcome(l);
			h = '<span class="' + resClass(res) + '">Last game: ' + res + ' vs ' + opponents(l) + (l.result ? ' (' + l.result + ')' : '') + '</span>';
			dismiss = function () { keep('rfog.seen', l.match); banner(); };
		}
		if (!h || inGame && !NET.displaced) { el.classList.add('hidden'); el.innerHTML = ''; return; }
		if (dismiss) { h += '<button class="link x" aria-label="dismiss">✕</button>'; }
		el.innerHTML = h;
		el.classList.remove('hidden');
		var b = el.querySelector('.primary');
		if (b && act) { b.onclick = act; }
		var x = el.querySelector('.x');
		if (x && dismiss) { x.onclick = dismiss; }
	}

	// ---- online: the account page ---------------------------------------------
	var ACCT_ACTIONS = {
		guest: [['save', 'Save my progress', 'make this guest an account, keeping everything', ['name', 'password']],
			['switch', 'Log in to another account', '', []]],
		account: [['password', 'Change password', '', ['current', 'new password']],
			['recovery', 'New recovery code', 'the old one stops working', ['password']],
			['logout', 'Sign out of this device', '', []],
			['logout_all', 'Sign out everywhere', '', []],
			['switch', 'Log in to another account', '', []],
			['delete', 'Delete my account', 'ratings, history, everything; asks twice', ['password']]]
	};
	function accountHTML() {
		var w = NET.welcome, a = NET.acct;
		var acts = ACCT_ACTIONS[w.guest ? 'guest' : 'account'];
		var h = '<p class="sub"><b>' + w.name + '</b>' + (w.guest ? ' (guest)' : '') + '</p>';
		if (a.form) {
			var act = acts.filter(function (x) { return x[0] === a.form; })[0];
			h += '<p><b>' + act[1] + '</b></p>';
			act[3].forEach(function (lbl, i) {
				var secret = lbl !== 'name';
				h += '<label class="fld"><span>' + lbl + '</span><input id="af' + i + '" type="' + (secret ? 'password' : 'text') + '" value="' + (lbl === 'name' ? w.name : '') + '" autocomplete="off" autocapitalize="off"></label>';
			});
			h += '<button id="afgo" class="primary' + (a.form === 'delete' ? ' danger' : '') + '">' + (a.form === 'delete' && !a.sure ? 'Delete' : 'Confirm') + '</button><button id="afback" class="link">back</button>';
		} else {
			h += '<div class="acts">';
			acts.forEach(function (x) { h += '<button data-act="' + x[0] + '"' + (x[0] === 'delete' ? ' class="danger"' : '') + '><b>' + x[1] + '</b>' + (x[2] ? '<span>' + x[2] + '</span>' : '') + '</button>'; });
			h += '</div><button id="acctback" class="link">back</button>';
			h += '<p class="small">an account is a name and a password; nothing else is asked or kept</p>';
		}
		if (a.notice) { h += '<p class="good">' + a.notice + '</p>'; }
		if (NET.err) { h += '<p class="note">' + NET.err + '</p>'; }
		return h;
	}
	function wireAccount(box) {
		if (!NET.acct) { return; }
		var a = NET.acct;
		each(box, '[data-act]', function (b) {
			b.onclick = function () {
				var id = b.dataset.act;
				NET.err = '';
				a.notice = '';
				if (id === 'switch') { NET.acct = null; askSignIn('login'); return; }
				if (id === 'logout' || id === 'logout_all') { nsend('account', { action: id, token: stored(TOKEN) || '' }); return; }
				a.form = id;
				a.sure = false;
				renderOnline();
			};
		});
		var back = document.getElementById('acctback');
		if (back) { back.onclick = function () { NET.acct = null; NET.err = ''; renderOnline(); }; }
		var fb = document.getElementById('afback');
		if (fb) { fb.onclick = function () { a.form = null; NET.err = ''; renderOnline(); }; }
		var go = document.getElementById('afgo');
		if (go) {
			go.onclick = function () {
				var v = function (i) { var e = document.getElementById('af' + i); return e ? e.value : ''; };
				if (a.form === 'delete' && !a.sure) {
					a.sure = true;
					NET.err = 'this deletes your account, ratings and history; confirm to go on';
					go.textContent = 'Confirm';
					var nt = box.querySelector('.note');
					if (nt) { nt.textContent = NET.err; } else { box.insertAdjacentHTML('beforeend', '<p class="note">' + NET.err + '</p>'); }
					return;
				}
				var req = { action: a.form };
				if (a.form === 'save') { req.name = v(0).trim(); req.password = v(1); }
				else if (a.form === 'password') { req.password = v(0); req.new_password = v(1); }
				else { req.password = v(0); }
				nsend('account', req);
			};
		}
	}
	function accountDone(d) {
		var a = NET.acct || { form: null };
		NET.err = '';
		a.form = null;
		switch (d.action) {
			case 'save':
				NET.welcome.name = d.name;
				NET.welcome.guest = false;
				keep(NAME, d.name);
				NET.code = d.recovery || '';
				a.notice = 'progress saved: you are ' + d.name;
				if (NET.wantSignin) { NET.wantSignin = false; NET.acct = null; renderOnline(); return; } // registered from the sign-in card
				break;
			case 'password':
				a.notice = 'password changed';
				break;
			case 'recovery':
				NET.code = d.recovery || '';
				break;
			case 'logout': case 'logout_all': case 'delete':
				keep(TOKEN, null);
				if (NET.ws) { try { NET.ws.onclose = null; NET.ws.close(); } catch (e) { /* gone */ } }
				NET.ws = null;
				NET.welcome = null;
				NET.acct = null;
				NET.wantSignin = false;
				NET.history = [];
				if (NET.local) { C = NET.local; NET.local = null; }
				toast(d.action === 'delete' ? 'account deleted' : d.action === 'logout_all' ? 'signed out everywhere' : 'signed out');
				connect(anonAuth());
				return;
		}
		NET.acct = a;
		renderOnline();
		if (!$('prefs').classList.contains('hidden')) { renderPrefs(); }
	}

	// ---- online: the draft ----------------------------------------------------
	// The pick screen, filled from the server's draft: each draft_state says
	// every pick and ban so far, whose step it is and its deadline.
	function showDraft() {
		var d = NET.draft, m = NET.match;
		if (!d || !m) { return; }
		var ps = (m.players || []).slice().sort(function (a, b) { return a.slot - b.slot; });
		var mt = (ps.filter(function (p) { return p.slot === m.you; })[0] || { team: 0 }).team;
		var opened = !PK || PK.match !== d.match;
		if (opened) {
			openPicks({ match: d.match, heroes: d.heroes, tag: clockLabel(m.time) + ' · ' + m.mode + ' · draft',
				seats: ps.map(function (p) { return { slot: p.slot, name: p.slot === m.you ? 'you' : p.bot ? 'bot' : p.name, team: p.team === mt ? 0 : 1, me: p.slot === m.you }; }),
				lock: function (id) {
					if (PK.kind === 'pick') { pick.hero = id; } // the preference for the next queue, too
					PK.sent = true;
					drawPicks();
					nsend('draft', { match: m.match, kind: PK.kind, hero: id });
				} });
		}
		var o = PK, picks = d.picks || {};
		// picks new since the last state show one by one, in draft order (the
		// server sends the bots' in one go); a tap shows the rest. The seats
		// hold the truth at once, so what is taken is never behind.
		clearTimeout(o.timer);
		var fresh = opened || still() ? [] : (d.order || []).map(function (st) { return o.seats.filter(function (s) { return st.kind === 'pick' && s.slot === st.slot && (!s.hero || s.wait) && picks[s.slot]; })[0]; }).filter(Boolean);
		o.seats.forEach(function (s) { s.hero = picks[s.slot] || null; s.wait = fresh.indexOf(s) > 0; });
		fresh.shift();
		var fill = function () { fresh.shift().wait = false; };
		var step = function () {
			if (PK !== o) { return; }
			if (fresh.length) { fill(); drawPicks(); }
			if (fresh.length) { o.timer = setTimeout(step, 420); return; }
			o.cut = null;
			if (o.start) { hold(function () { o.started = true; turnStart(o.start); }); }
		};
		o.cut = function () { clearTimeout(o.timer); while (fresh.length) { fill(); } drawPicks(); step(); };
		o.go = step;
		o.banned = d.banned || [];
		o.kind = d.kind;
		o.turn = d.kind === 'done' ? -1 : o.seats.map(function (s) { return s.slot; }).indexOf(d.turn);
		o.sent = false;
		o.err = '';
		o.note = 'starting…';
		// your pick: the commander you play by choice, if it is free; never
		// one picked for you to ban
		if (o.seats[o.turn] && o.seats[o.turn].me && !canLock(o.sel)) { o.sel = d.kind === 'pick' && canLock(pick.hero) ? pick.hero : null; }
		show('draft');
		drawPicks();
		clock('draftclock', d.deadline);
		if (fresh.length) { o.timer = setTimeout(step, 420); } else { o.cut = null; if (o.start) { step(); } }
	}
	// clock counts down to a deadline in an element, every second.
	var clockTimer = 0;
	function clock(id, deadline) {
		clearInterval(clockTimer);
		var end = deadline ? Date.parse(deadline) : 0;
		var tickc = function () {
			var el = $(id);
			if (!el || !end) { return; }
			var s = Math.max(0, Math.round((end - Date.now()) / 1000));
			el.textContent = s > 3600 ? Math.round(s / 3600) + ' h left' : Math.floor(s / 60) + ':' + ('0' + s % 60).slice(-2);
			el.classList.toggle('low', s <= 10);
		};
		tickc();
		if (end) { clockTimer = setInterval(tickc, 1000); }
	}

	// ---- online: the match ----------------------------------------------------
	function turnStart(b) {
		var m = NET.match || {};
		var fresh = !S || !S.online || S.online.match !== b.match;
		call('remote', JSON.stringify(b.view), m.you || 0);
		if (fresh) {
			var me0 = (b.view.Players || []).filter(function (p) { return p.ID === m.you; })[0] || { Team: 0 };
			S = { view: b.view, sel: 0, orders: [], moves: [], targets: [], abil: null, abilTargets: [], busy: false, look: 0, last: {}, scars: {},
				you: m.you || 0, team: me0.Team, online: { match: b.match, time: m.time || 'online', committed: false } };
			$('sheet').classList.add('hidden');
			show('game');
			layout();
		} else {
			S.view = b.view;
			S.orders = [];
			S.sel = 0;
			S.moves = [];
			S.targets = [];
		}
		S.online.turn = b.turn;
		S.online.deadline = b.deadline;
		S.online.committed = !!b.resumed && NET.waitingOn === b.match;
		S.online.async = !!m.async;
		NET.waitingOn = null;
		render();
		clock('clock', b.deadline);
	}
	function resolvedOnline(b) {
		if (!S || !S.online || S.online.match !== b.match) { return; }
		clearInterval(clockTimer);
		S.busy = true;
		$('sheet').classList.add('hidden');
		S.sel = 0;
		S.moves = [];
		S.targets = [];
		S.abil = null;
		S.orders = [];
		S.log = logLines(b.events || [], S.view);
		S.last = {};
		S.scars = {};
		(b.events || []).forEach(function (e) {
			if (e.k !== 'Moved') { return; }
			var to = e.path && e.path.length ? e.path[e.path.length - 1] : e.to;
			S.last[e.from.X + ',' + e.from.Y] = true;
			S.last[to.X + ',' + to.Y] = true;
		});
		render();
		S.post = b.view;
		play(b.events || [], function () {
			S.view = b.view;
			S.busy = false;
			S.look = 0;
			S.online.committed = false;
			render();
			if (NET.pendingEnd) { var e = NET.pendingEnd; NET.pendingEnd = null; matchEnd(e); return; }
			if (NET.pending) { var p = NET.pending; NET.pending = null; turnStart(p); }
		});
	}
	function matchEnd(b) {
		clearInterval(clockTimer);
		var m = NET.match || {}, you = m.you || 0;
		var team = S ? S.team : 0;
		var won = b.winner === team, draw = b.winner < 0;
		// a quick-paired match is played again with one tap: the same clock
		var mode = NET.mode && m.time && m.time.replace(/-casual$/, '') === NET.mode && !isDaily(NET.mode) ? NET.mode : '';
		var live = function () { nsend('live', {}); };
		var hero = S && S.view ? commanderOf(S.you) : '';
		NET.match = null;
		NET.draft = null;
		NET.joined = false;
		NET.state = 'lobby';
		result({ title: draw ? 'DRAW' : won ? 'SIGNAL HELD' : 'SIGNAL LOST', res: draw ? 'draw' : won ? 'won' : 'lost',
			score: b.final && b.final.Teams ? [b.final.Teams[team].Score, b.final.Teams[1 - team].Score] : null,
			sub: 'you – ' + opponents(m) + (b.result ? ' · ' + b.result : '') + (b.reason ? ' · ' + b.reason : ''),
			rating: b.ratings && b.ratings[you], hero: hero, label: 'Play again · ' + mode,
			again: mode ? function () { live(); quickPair(mode); } : null, menu: live });
	}

	window.addEventListener('resize', function () { BW = 0; backdrops(); if (S && !S.busy && !$('game').classList.contains('hidden')) { layout(true); render(); } });
})();
