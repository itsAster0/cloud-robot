// Cloud Computing project review — minimal handout version.
// Fill the [bracketed] placeholders (team, dates, guide) before submitting.
#set document(title: "Multiplayer Robot Arena — Cloud Computing Review 1 (Minimal)", author: ("Team placeholders",))
#set page(paper: "a4", margin: (x: 2.4cm, y: 2.2cm), numbering: "1", number-align: center)
#set text(size: 10.5pt, font: ("Libertinus Serif", "DejaVu Sans Mono"))
#set par(justify: true, leading: 0.62em)
#show heading.where(level: 1): set text(size: 13pt)
#show heading.where(level: 1): it => v(6pt) + it + v(2pt)

#let accent = rgb("#1f3d33")
#let soft = rgb("#5a6b62")
#let nd(body, fill: rgb("#f2f6f4")) = rect(width: 100%, fill: fill, stroke: 0.6pt + accent, inset: 7pt, radius: 3pt, align(center, body))
#let wide(body, fill: rgb("#e7f0ea")) = grid.cell(colspan: 3, nd(body, fill: fill))
#let wire(body) = grid.cell(colspan: 3, align(center, text(size: 8pt, fill: soft, body)))

#align(center)[
  #text(size: 17pt, weight: "bold", fill: accent)[Cloud Computing Project — Review 1]
  #v(4pt)
  #text(size: 13pt)[Multiplayer Robot Arena: programmable robot combat as a cloud service]
]

= Project Title

*Multiplayer Robot Arena.* A browser-based multiplayer arena where every player
programs a personal robot in Lua 5.4, deploys it from a persistent SSH
development box, and watches the server-authoritative simulation live.

= Team Details

#table(
  columns: (1fr, auto, auto, 1fr),
  inset: 6pt,
  stroke: 0.5pt + luma(160),
  table.header([*Name*], [*Roll number*], [*Branch*], [*Role*]),
  [Team member 1], [21XXXXXXX], [CSE], [Engine, bots, maps],
  [Team member 2], [21XXXXXXX], [CSE], [API, queue, cloud adapters],
  [Team member 3], [21XXXXXXX], [CSE], [Web client, spectating, docs],
  [Team member 4], [21XXXXXXX], [CSE], [Boxes, provisioning, Lua SDK],
)

= Review Details

#table(
  columns: (auto, auto),
  inset: 6pt,
  stroke: 0.5pt + luma(160),
  [*Review*], [Review 1],
  [*Course*], [Cloud Computing],
  [*Date*], [DD Month 2026],
  [*Faculty guide*], [Guide name],
)

= Introduction

Players sign in, receive one persistent Docker SSH box, and edit
`/workspace/main.lua` over SSH. Registration snapshots the script to S3 and
starts an agent that keeps one authenticated outbound WebSocket to a Go API.
The API runs a deterministic 10 Hz simulation and fans versioned snapshots out
to every browser, so spectators and players see the same match in near real
time. AWS-compatible services (S3, DynamoDB, SQS) are emulated locally with
Floci, keeping the whole stack deployable later to a single VPS.

= Problem Statement

Programming-game platforms usually run all robot code on one desktop machine,
which gives no cloud architecture to learn from and no shared live view. The
project must instead run untrusted player scripts inside per-player boxes,
keep all game state server-authoritative, stream the match to any browser,
and persist snapshots, match jobs, and results through AWS-compatible services
— entirely on student hardware.

= Objectives

+ A working loop: provision box → edit Lua → register → live synchronized match.
+ Server-authoritative, seed-deterministic engine (no trusted client state).
+ One resource-limited SSH box per player; robot token never reaches the browser.
+ AWS-compatible persistence (S3 snapshots, DynamoDB state, SQS match jobs) behind small adapters.
+ Public spectator surface: live view, history, replays, leaderboard, profiles.

= Literature review

- *Robocode / Battlecode / Halite / CodinGame* — programming-game arena concept; ours adds per-player cloud boxes and live spectating.
- *Gambetta, Fast-Paced Multiplayer; Bernier, GDC 2001* — server authority and client interpolation; we apply both at a 10 Hz tick.
- *LocalStack / Floci* — emulate AWS locally so no real account is needed.
- *Agones (Google)* — Kubernetes game-server fleets; deliberately heavier than our single-VPS scope.

= Architectural diagram

#grid(
  columns: (1fr, 1fr, 1fr),
  gutter: 8pt, row-gutter: 5pt,
  nd[*Viewer browser* \ Svelte 5], nd[*Player browser A* \ Svelte 5 + WorkOS], nd[*Player browser B* \ Svelte 5],
  wire[↑ REST + WebSocket · 10 Hz snapshots down, intents up ↓],
  wide[*Go API* — auth · REST · WebSocket hub · duel queue · match lifecycle],
  wire[↓ sim ticks · agent sockets · AWS-compatible calls · Docker socket ↓],
  nd[*Engine* \ deterministic 10 Hz sim], nd[*Provisioner* \ box lifecycle], nd[*Floci* \ S3 · DynamoDB · SQS],
  wire[↓ container lifecycle ↑ `main.lua` snapshot ↑],
  wide[*Robot boxes ×N (Docker)* — persistent SSH workspace + Lua 5.4 agent, one outbound authenticated WSS to the API],
)

= Modules

Web client (`apps/web`) · API server (`cmd/server`, `internal/api`) ·
simulation engine and bots (`internal/engine`) · Lua runtime and SDK
(`internal/robotlua`, `sdk/lua`) · box provisioning and supervisor
(`internal/boxes`, `cmd/provisioner`, `cmd/box-supervisor`) · cloud adapters
(`internal/cloud`) · auth (`internal/auth`) · data model (`internal/model`) ·
curated script templates (`internal/scripts`).

= Status of work

- ✓ End-to-end demo: two boxes, two browsers, synchronized 10 Hz snapshots.
- ✓ Persistent boxes with SSH editing; snapshots to S3; restart-safe stack.
- ✓ Deterministic engine: maps, obstacles, items, weapons, mines, turrets, zone collapse, overtime.
- ✓ Bots: four difficulties × four personalities (aggressive/evasive/camper/mixed), anti-stuck wandering.
- ✓ Duel queue with bot fill, squad 5v5, solo free-for-all; replays, ratings, leaderboard, public spectating.

= Next review status

- Obstacle-aware bot movement (grid A\* or potential fields).
- Match-creatable arena size picker with clamps.
- Follow-cam and auto-director spectator modes.
- Share preview cards and supply airdrop events.

= Conclusion

The Phase 1 loop is demonstrable end to end on local AWS-compatible
infrastructure: programmable robots, an authoritative deterministic server,
and a watchable, replayable match surface. The next review focuses on
smarter navigation and richer viewing, keeping the single-VPS deploy story.

= References

+ M. Nelson, "Robocode," IBM alphaWorks, 2001. #link("https://robocode.sourceforge.io")[robocode.sourceforge.io]
+ MIT Battlecode, annual programming competition. #link("https://battlecode.org")[battlecode.org]
+ Two Sigma, "Halite AI programming challenge," 2016–2018. #link("https://halite.io")[halite.io]
+ G. Gambetta, "Fast-Paced Multiplayer," 2014. #link("https://gabrielgambetta.com/client-server-game-architecture.html")[gabrielgambetta.com]
+ Y. Bernier, "Latency Compensating Methods in Client/Server In-game Protocol Design," GDC 2001.
+ LocalStack — local AWS cloud stack emulator. #link("https://localstack.cloud")[localstack.cloud]
+ Google, "Agones — game server hosting on Kubernetes." #link("https://agones.dev")[agones.dev]
+ Lua 5.4 Reference Manual, PUC-Rio. #link("https://www.lua.org/manual/5.4/")[lua.org/manual/5.4]
