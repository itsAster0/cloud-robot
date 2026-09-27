-- Facts derived from one observation. Keeping perception separate from
-- decisions makes plan.lua easy to read and to test without a server.
local arena = require "arena"
local M = {}

-- Centre of the lens where two overlapping circles meet: inside both, so a
-- robot waiting there is safe now and after the zone moves.
function M.lens(a, b)
  local d = arena.distance(a, b)
  if d < 1 then return { x = a.x, y = a.y } end
  local ux, uy = (b.x - a.x) / d, (b.y - a.y) / d
  local near = { x = b.x - ux * b.radius, y = b.y - uy * b.radius }
  local far = { x = a.x + ux * a.radius, y = a.y + uy * a.radius }
  return { x = (near.x + far.x) / 2, y = (near.y + far.y) / 2 }
end

function M.read(obs)
  local self = obs.self
  local facts = {
    tick = obs.tick,
    hp = self.hp / math.max(1, self.maxHp or 100),
    target = arena.best_target(obs, { range = 850 }),
    uplink = arena.uplink(obs),
    heal_slot = arena.find_consumable(obs, "medkit") or arena.find_consumable(obs, "repair_pack"),
  }
  local zone = obs.zone
  if zone and zone.active then
    facts.zone = zone
    facts.inside = arena.distance(self, zone) <= zone.radius - 80
    if zone.next then
      facts.next = zone.next
      facts.inside_next = arena.distance(self, zone.next) <= zone.next.radius - 80
      facts.arrives_in = zone.next.arrivesAt - obs.tick
      facts.lens = M.lens(zone, zone.next)
    end
  end
  -- The richest visible bounty: ending a long streak is worth the risk.
  for _, robot in ipairs(obs.robots or {}) do
    if robot.alive ~= false and robot.team ~= self.team and (robot.bounty or 0) > 0 and not robot.protected then
      if not facts.bounty or robot.bounty > facts.bounty.bounty then facts.bounty = robot end
    end
  end
  return facts
end

return M
