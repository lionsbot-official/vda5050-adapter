# LionsBot VDA5050 Adapter

Makes a LionsBot cleaning robot speak **VDA5050 2.0.0** to a fleet master over MQTT.
The adapter is the **vehicle side** of the protocol: your fleet control talks to it
exactly as it would to any VDA5050 vehicle, and the adapter translates orders and
instant actions into robot commands (and robot telemetry back into VDA5050 `state`).

You do not talk to the robot directly. You publish standard VDA5050 messages to the
robot's MQTT topics; the adapter does the rest.

- Protocol: VDA5050 2.0.0 (`vda5050/v2/...`); this branch cannot be relabeled as another edition.
- Corridors, map distribution, `maps`, `errorHint`, `vehicleConfig` and factsheet `blockingTypes` belong to later editions and are not exposed by this line.
- Transport: MQTT (any broker; the adapter is an MQTT client)
- Messages: standard VDA5050 JSON — see the [final January 2022 specification](reference/VDA5050-2.0.0/VDA5050_EN.pdf)
  (a copy ships in [`reference/VDA5050-2.0.0/`](reference/VDA5050-2.0.0/))

---

## Choose a protocol edition

Each branch contains one implementation. `MASTER_VERSION` must match that branch;
it cannot switch protocol editions.

| VDA5050 edition | Branch | Initial release tag | Adapter fix example |
|---|---|---|---|
| 3.0.0 | `main` | `v3.0.0` | `v3.0.0+adapter.1` |
| 2.1.0 | `vda-2.1.0` | `v2.1.0` | `v2.1.0+adapter.1` |
| 2.0.0 | `vda-2.0.0` | `v2.0.0` | `v2.0.0+adapter.1` |

Select a published tag for deployment and build from its source checkout or release
archive. These are source release labels, not Go module major-version paths.
The older `v1.0.0` release also implements VDA5050 3.0.0; it keeps its original name.
See each edition's README and factsheet for its supported capabilities.

## 1. Quick start

You need Go 1.26+, an MQTT broker your fleet master uses, and LionsBot account
credentials for the robot.

```bash
cp .example.env .env          # fill in your values (see below)
set -a; . ./.env; set +a      # export the vars (envconfig reads process env, not the file)
go run .
```

Configuration (env vars — full list with defaults in [`.example.env`](.example.env)):

| Var | Meaning |
|---|---|
| `MASTER_HOST`, `MASTER_PORT` | your fleet-master MQTT broker |
| `MASTER_USERNAME`, `MASTER_PASSWORD` | broker credentials (leave empty for an open broker) |
| `MASTER_VERSION` | protocol version, `2.0.0` |
| `MASTER_CLIENT_ID` | MQTT client-id prefix (the robot serial is appended, so instances don't collide) |
| `ROBOT_REGION` | `asia` \| `europe` \| `us` \| `australia` — derives all robot endpoints |
| `ROBOT_EMAIL`, `ROBOT_PASSWORD` | LionsBot account credentials |
| `ROBOT_ROBOT_ID` | the robot's serial — becomes the VDA5050 **`serialNumber`** on every topic below |
| `ROBOT_MODEL`, `ROBOT_ROBOT_TYPE` | robot model/family (e.g. `R5`) |
| `ROBOT_THETA_OFFSET_DEG` | heading offset (degrees) if the robot's map frame is rotated |
| `ROBOT_MAP_POLL_SECONDS` | active-map metadata poll interval (default 60) |

`ROBOT_STATUS_URL` / `ROBOT_POSE_URL` / `ROBOT_LOGIN_URL` / `ROBOT_API_BASE` override
individual endpoints when set; otherwise they come from `ROBOT_REGION`.

Once running, the adapter publishes a retained `ONLINE` on the `connection` topic and
a retained `factsheet` — subscribe with any MQTT client to confirm it's up (§3).

---

## 2. MQTT interface

All topics follow `vda5050/v2/<manufacturer>/<serialNumber>/<endpoint>`, where
`<manufacturer>` is always **`lionsbot`** and `<serialNumber>` is your `ROBOT_ROBOT_ID`.
Examples below use `R3-0001` — substitute your serial.

| Endpoint | Direction | QoS | Retained | Purpose |
|---|---|---|---|---|
| `connection` | adapter → master | 1 | yes | `ONLINE` / `OFFLINE`; last-will `CONNECTIONBROKEN` on ungraceful drop |
| `factsheet` | adapter → master | 0 | yes | robot capabilities (see §3) |
| `state` | adapter → master | 0 | no | live state, ~1 s heartbeat + immediately on change |
| `cleaningcatalog` | adapter → master | 0 | yes | **vendor extension**: cleanable zones, cleaning modes, docks for the active map |
| `order` | master → adapter | 0 | — | drive / clean the robot (§4) |
| `instantActions` | master → adapter | 0 | — | dock, undock, pause, cancel, … (§4) |

Every message carries the standard VDA5050 header: `headerId` (you increment per topic),
`timestamp` (ISO8601 UTC `YYYY-MM-DDTHH:mm:ss.ffZ`), `version` (`2.0.0`), `manufacturer`
(`lionsbot`), `serialNumber`. **The adapter routes off the topic, not the body** — the
serial in the topic is what matters.

Examples use [mosquitto](https://mosquitto.org/) (`-h`/`-p`/`-u`/`-P` = your broker):

```bash
BROKER="-h localhost -p 1883"
SER="R3-0001"
```

---

## 3. Discover what the robot can do

The `factsheet` is **retained**, so just subscribe and you get it immediately — even if
the adapter came online before you:

```bash
mosquitto_sub $BROKER -t "vda5050/v2/lionsbot/$SER/factsheet" -v
```

It lists supported actions (`protocolFeatures.agvActions`), physical parameters,
and localization/navigation types. You can also request a fresh copy any time with the
`factsheetRequest` instant action (§4.7).

Valid **cleaning zone names** and modes come from the retained `cleaningcatalog` topic —
subscribe to it before building a clean order:

```bash
mosquitto_sub $BROKER -t "vda5050/v2/lionsbot/$SER/cleaningcatalog" -v
```

---

## 4. Operating the robot

| You want to… | Send | How |
|---|---|---|
| **Move** to a point | `order` with node positions | §4.1 |
| **Clean** zones | `order` with a `startCleaning` node action | §4.2 |
| **Dock** (return to charger) | `startCharging` instant **or** node action (pausable, cancellable) | §4.3 |
| **Undock** | `stopCharging` instant/node action (or just send a move order) | §4.4 |
| **Pause / Resume** | `startPause` / `stopPause` instant actions | §4.5 |
| **Cancel** the current order | `cancelOrder` instant action | §4.5 |
| **Relocalize** (reset a lost pose) | `initPosition` instant action | §4.6 |
| **Get the factsheet** | `factsheetRequest` instant action | §4.7 |

Publish orders to `.../order` and instant actions to `.../instantActions`.

### 4.1 Move (point-to-point)

The robot navigates freely to each **released** node that has a `nodePosition`, in
`sequenceId` order. Node `sequenceId` 0 is "you are here" (the decision point); put the
destination on the next node. Coordinates are metres in the robot's map frame; `mapId`
must be the robot's current map (see `state.agvPosition.mapId`). `theta`
(radians) is optional.

```bash
mosquitto_pub $BROKER -t "vda5050/v2/lionsbot/$SER/order" -m '{
  "headerId": 1, "timestamp": "2026-07-20T14:30:00.00Z", "version": "2.0.0",
  "manufacturer": "lionsbot", "serialNumber": "R3-0001",
  "orderId": "order-1", "orderUpdateId": 0,
  "nodes": [
    { "nodeId": "n0", "sequenceId": 0, "released": true, "actions": [] },
    { "nodeId": "n1", "sequenceId": 2, "released": true, "actions": [],
      "nodePosition": { "x": 3.0, "y": 1.5, "theta": 0, "mapId": "map-1" } }
  ],
  "edges": [
    { "edgeId": "e1", "sequenceId": 1, "released": true,
      "startNodeId": "n0", "endNodeId": "n1", "actions": [] }
  ]
}'
```

Notes:
- The robot is treated as having **reached** a node when it is within the node's
  `allowedDeviationXY` radius. Omitted or explicit `0` uses the adapter's **0.5 m** normal-tolerance default; a positive value sets the radius in metres.
  VDA defines zero in terms of normal manufacturer tolerance. The current 0.5 m
  value requires hardware calibration confirmation before deployment.
- Navigation is **node-position based** — the robot plans its own path; edge geometry
  (`trajectory`, …) and unimplemented motion constraints are rejected.
- Multi-stop: add more released nodes (sequenceIds 4, 6, …) with positions. The adapter
  drives them one at a time and reports progress via `lastNodeId` in `state`.

### 4.2 Clean

Cleaning is a **node action** (`startCleaning`) on a single positionless node —
the robot cleans the named zones and stays put when done. Zone names come from
`cleaningcatalog`.

```bash
mosquitto_pub $BROKER -t "vda5050/v2/lionsbot/$SER/order" -m '{
  "headerId": 1, "timestamp": "2026-07-20T14:30:00.00Z", "version": "2.0.0",
  "manufacturer": "lionsbot", "serialNumber": "R3-0001",
  "orderId": "clean-1", "orderUpdateId": 0,
  "nodes": [
    { "nodeId": "n0", "sequenceId": 0, "released": true, "actions": [
      { "actionId": "a1", "actionType": "startCleaning", "blockingType": "HARD",
        "actionParameters": [
          { "key": "zones", "value": [ { "name": "Lobby" }, { "name": "Corridor" } ] },
          { "key": "bypassBlackout", "value": false }
        ] }
    ] }
  ],
  "edges": []
}'
```

`startCleaning` parameters:

| Key | Type | Meaning |
|---|---|---|
| `zones` | array | one entry per zone: `{ "name": "<zone>" }`, plus optional per-zone `"cleaningMode": "<mode>"` and `"configs": { "<setting>": <number> }` overrides. Omit the overrides to use the zone's catalog/robot default. |
| `bypassBlackout` | bool | run even during a configured blackout window |
| `homePoi` | object with nonempty `name` | legacy compatibility only; ignored, robot stays in place after cleaning |

Track the clean via its `actionState` in `state.actionStates`
(`WAITING` → `RUNNING` → `FINISHED`/`FAILED`).

### 4.3 Dock (return to charger)

The robot's dock **is** its charger. Docking navigates to the nearest dock and docks.
Send `startCharging` as an **instant action** for a quick dock:

```bash
mosquitto_pub $BROKER -t "vda5050/v2/lionsbot/$SER/instantActions" -m '{
  "headerId": 1, "timestamp": "2026-07-20T14:30:00.00Z", "version": "2.0.0",
  "manufacturer": "lionsbot", "serialNumber": "R3-0001",
  "actions": [ { "actionId": "dock-1", "actionType": "startCharging", "blockingType": "HARD" } ]
}'
```

Or as a **node action** in an order, so "go dock" is a planned step (single
self-navigating node, same shape as a clean):

```jsonc
"nodes": [ { "nodeId": "n0", "sequenceId": 0, "released": true, "actions": [
  { "actionId": "dock-1", "actionType": "startCharging", "blockingType": "HARD" } ] } ]
```

An in-flight dock is **pausable** (`startPause` → `stopPause`) and **cancellable**
(`cancelOrder` aborts the approach). Track it via its `actionState` (node scope) or
`actionStates` (instant scope). While docked, `state.batteryState` shows charging.
The factsheet advertises `startCharging` at INSTANT+NODE with `pauseAllowed` and
`cancelAllowed` true.

### 4.4 Undock

Explicitly:

```bash
mosquitto_pub $BROKER -t "vda5050/v2/lionsbot/$SER/instantActions" -m '{
  "headerId": 1, "timestamp": "2026-07-20T14:30:00.00Z", "version": "2.0.0",
  "manufacturer": "lionsbot", "serialNumber": "R3-0001",
  "actions": [ { "actionId": "undock-1", "actionType": "stopCharging", "blockingType": "HARD" } ]
}'
```

Or **implicitly**: if you send a **move order while the robot is docked, it undocks
automatically first**, then drives. You don't have to undock by hand before a move.

`stopCharging` is available at INSTANT+NODE scope, but — unlike docking — undock
**cannot be paused or cancelled once started** (the robot exposes no undock control).
The factsheet advertises `stopCharging` with `pauseAllowed`/`cancelAllowed` false.

### 4.5 Pause, Resume, Cancel

All instant actions (same envelope as above; showing just `actions`):

```jsonc
// Pause the current move or clean
"actions": [ { "actionId": "p1", "actionType": "startPause", "blockingType": "NONE" } ]

// Resume
"actions": [ { "actionId": "r1", "actionType": "stopPause", "blockingType": "NONE" } ]

// Cancel the current order
"actions": [ { "actionId": "c1", "actionType": "cancelOrder", "blockingType": "NONE" } ]
```

Pause/resume route by what's running — a move pauses/resumes movement, a clean
pauses/resumes cleaning. Cancelling an idle robot
fails with a `noOrderToCancel` error in `state.errors`.

### 4.6 Relocalize (reset the pose)

When the robot reports `agvPosition.positionInitialized = false` (lost localization),
reset its pose with a known position on its current map:

```jsonc
"actions": [ { "actionId": "ip-1", "actionType": "initPosition", "blockingType": "HARD",
  "actionParameters": [
    { "key": "x", "value": 12.5 }, { "key": "y", "value": 3.0 },
    { "key": "theta", "value": 1.57 }, { "key": "mapId", "value": "map-1" },
    { "key": "lastNodeId", "value": "n0" }
  ] } ]
```

`x`/`y` are metres and `theta` radians in the site frame (same convention as node
positions). The action reaches `FINISHED` when the robot re-localizes, or `FAILED`
(with the localization score in the reason) when it cannot. This version is
single-map: a `mapId` that is not the robot's current map is rejected, and it is
rejected while an order (move/clean/dock) is in flight.

### 4.7 Request the factsheet

```jsonc
"actions": [ { "actionId": "fs-1", "actionType": "factsheetRequest", "blockingType": "NONE" } ]
```

Re-publishes the `factsheet` and reports the action `FINISHED`.

---

## 5. Watching state

Subscribe to `state` and watch these fields:

```bash
mosquitto_sub $BROKER -t "vda5050/v2/lionsbot/$SER/state" -v
```

| Field | Tells you |
|---|---|
| `driving` | robot is moving |
| `lastNodeId`, `lastNodeSequenceId` | last node reached (traversal progress) |
| `agvPosition` | live `{x, y, theta, mapId}` and localization status (omitted until a usable map frame is available) |
| `actionStates[]` | order and instant action lifecycle; terminal instant actions clear on a new order, running actions remain |
| `batteryState` | battery level, charging/docked |
| `paused` | pause mode active |
| `operatingMode`, `safetyState` | robot mode / e-stop status |
| `errors[]` | e.g. `validationError`, `NODE_UNREACHABLE`, `noOrderToCancel`, `INVALID_INSTANT_ACTION` |

---

## 6. Order rules (what the adapter accepts)

- **Shape**: at least one node; exactly `nodes − 1` edges.
- **New order**: must contain a **released** node at `sequenceId` 0.
- **Movement**: only nodes that are `released` **and** have a `nodePosition` are driven;
  nodes beyond sequence 0 require a position. A single sequence-0 node can carry an action at the current location.
  Unreleased ("horizon") nodes are held until a later update releases them.
- **Order updates**: reuse the same `orderId` and increment `orderUpdateId`. The update's
  first node **must match the current decision point** (same `nodeId` **and** `sequenceId`
  as the last released node the robot is working from), otherwise the update is rejected
  with `orderError`. Appending nodes beyond the one in flight does **not**
  interrupt the current move.
- Unknown-map/localization and operating-mode transitions still require the release validation described below.

---

## 7. Scope

**Implemented:** connection, retained factsheet + `factsheetRequest`, live `state`
(pose, battery, action/order tracking, errors), orders (movement with
stitching/updates, node-action cleaning, node-action docking), instant actions
(`startCharging`/`stopCharging` = dock/undock — dock pausable + cancellable at node
scope, `startPause`/`stopPause`, `cancelOrder`, `initPosition` = relocalize),
and the `cleaningcatalog` vendor topic.

**Not implemented:** the `visualization` topic, navigation zone sets, trajectories, edge actions, mixed move/action orders, multiple node actions per order, cross-map
relocalization (`initPosition` is single-map only), and the `spotClean` action.

The V2.0 factsheet currently uses `CARRIER` with zero supported load mass; V2.0 has no cleaning class. This classification and the pinned physical parameters require integrator confirmation before deployment. Cleaning is the manufacturer action `startCleaning`. Motion actions accept `HARD`; control and factsheet actions accept `NONE`. V2.0 has no factsheet `blockingTypes` field; these requirements are stated here and enforced on intake. Orders containing node actions must contain one node and one action.

Orders require AUTOMATIC or SEMIAUTOMATIC mode with no active emergency stop, protective-field violation or fatal error. Navigation additionally requires an initialized pose on the requested map. Loss of localization stops active movement and retains its target for recovery; interrupted cleaning/docking waits for its completion event and then fails. Entering or leaving MANUAL clears held orders. Cancellation waits for confirmed stopping and for noninterruptible undocking to finish; duplicate instant action IDs are not dispatched twice.

Automated tests cover these behaviors; customer-master and real-robot acceptance remain outstanding. This is not a certification or conformance claim. See [CHANGELOG.md](CHANGELOG.md) for release history.

---

## Contributing

This repository is a published snapshot, updated on release. Issues and pull
requests are not monitored.

---

## License

[MIT](LICENSE). The bundled VDA repository Markdown and schemas carry the upstream
[license notice](reference/VDA5050-2.0.0/LICENSE.txt). The final PDF retains VDA
copyright and permits reprint with attribution; see the
[reference provenance](reference/VDA5050-2.0.0/README.md).
