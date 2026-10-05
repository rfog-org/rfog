# Ability data format

Each file holds `[[abilities]]` entries. Fields:

| field | meaning |
|---|---|
| `id` | unique ability id (referenced by heroes/units) |
| `name` | display name |
| `target` | `self` \| `unit` \| `tile` \| `all` |
| `range` | max Chebyshev distance from caster to target (unit/tile) |
| `area` | Chebyshev radius around the target tile (0 = single tile; 1 = 3×3; 2 = 5×5) |
| `filter` | which units effects touch: `enemies` \| `allies` \| `all` \| `self` (default by target) |
| `kind` | with `target = "all"`: restrict to this unit kind |
| `delay` | 0 = instant; N = resolves N turns later at the recorded target (telegraphed) |
| `cooldown` | turns |
| `visible` | for `tile` targets: target tile must be visible to the caster's team (default true) |
| `effects` | ordered list of effect primitives |

Effect primitives (`kind`) and their parameters:

| kind | params | notes |
|---|---|---|
| `damage` | `n` | 1 damage per point; shields absorb first |
| `heal` | `n` | up to max HP |
| `push` | `dist` | away from the ability's origin tile; blocked → `push_block_damage`; falls deal fall damage |
| `pull` | `dist` | toward the origin tile |
| `reveal` | `radius`, `turns` | tile effect visible to the caster's team |
| `cloak` | `turns` | invisible to enemies unless adjacent |
| `smoke` | `radius`, `turns` | tile effect: blocks LOS |
| `slow` | `mv`, `turns` | −mv movement |
| `root` | `turns` | cannot move |
| `shield` | `n`, `turns` | absorbs n damage |
| `teleport` | `who` (`self`/`target`), `to` (`target`/`adjacent_caster`) | |
| `stat` | `stat`, `delta`, `turns` | temporary stat change (hp mv jmp rng atk def ini vis) |
| `overwatch` | `rng`, `shots` | grants overwatch this turn (0 = unit's own RNG / 1 shot) |
| `mark` | `dice`, `turns` | attackers get +dice vs the marked unit |
| `spawn` | `unit`, `count` | spawns at target tile and nearest free tiles |
| `silence` | `turns` | cannot cast abilities |
| `taunt` | `turns` | must attack the caster if able |
| `approach` | `dist` | caster moves to a free tile adjacent to the target, ignoring terrain cost |
| `strike` | | caster performs a basic attack roll on the target |
| `sacrifice` | | removes the target unit (no XP awarded) |

Per-effect `radius` and `filter` override the ability's `area` and `filter` for that effect.
