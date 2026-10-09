-- Assault: close-range brawler. Engages from further away, fights near,
-- dashes to close the gap, drops mines while kiting, and only retreats when
-- badly hurt. Loadout: heavy, shotgun, reinforced_plating, mine_dispenser.
local arena = require "arena"
arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")), token = assert(os.getenv("ROBOT_TOKEN")),
  decide = arena.tactics({
    name = "ASSAULT", range = 700, preferred = 200, retreat_hp = 0.2, loot_reach = { 400, 200 },
    decorate = function(obs, action)
      if action.label == "ASSAULT_CLOSE_IN" and obs.self.energy > 40 then action.dash = true end
      if action.label == "ASSAULT_KITE" then action.utility = "mine_dispenser" end
    end,
  }),
})
