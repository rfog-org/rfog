package assets

// Cast maps each hero and unit to its character on the sprite sheet in
// src/tiny-dungeon (Kenney's Tiny Dungeon, CC0): a tile index, row*12+col,
// 16px tiles. tools/portraits cuts the terminal's portraits and board
// pieces from it; the graphical client draws the same characters.
var Cast = map[string]int{
	"hero_hask": 87, "hero_wren": 112, "hero_nul": 84, "hero_mott": 99, "hero_tally": 100,
	"unit_lineman": 96, "unit_ranged": 98, "unit_runner": 86, "unit_medic": 85, "unit_junkbot": 122,
	// Heroes and units drawn only as figures (units.go); these sheet
	// characters are the fallback when figures cannot be drawn.
	"hero_volt": 88, "hero_kite": 111, "hero_bram": 97, "hero_sable": 110, "hero_ordo": 101, "unit_drone": 123,
}

// SheetCols is how many tiles a row of the sheet holds.
const SheetCols = 12
