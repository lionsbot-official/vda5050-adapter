# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.0.0] - 2026-07-22

Initial public release: the vehicle side of VDA5050 3.0.0 for LionsBot cleaning
robots, as a standalone MQTT adapter.

### Added

#### Protocol (master-facing MQTT)

- Topic scheme `vda5050/v3/<manufacturer>/<serialNumber>/<endpoint>` with the
  spec-mandated QoS per topic (QoS 1 `connection`, QoS 0 elsewhere).
- `connection` topic: retained `ONLINE` on start, retained `OFFLINE` on graceful
  shutdown, `CONNECTION_BROKEN` last-will on ungraceful drop.
- Retained `factsheet` on connect, refreshable via the `factsheetRequest`
  instant action; advertises the full supported-action vocabulary, guarded by a
  drift test against the intake registry.
- `state` publishing: 1 s heartbeat plus immediate publish on relevant change
  (node traversal, action transitions, errors). Pose, battery, driving/paused,
  operating mode, safety state, action states, planned path, and VDA errors.
- Per-topic `headerId` counters; VDA timestamp format (UTC, millisecond).
- Mandatory VDA arrays always marshal as `[]`, never `null`.

#### Orders

- Order validation and acceptance: shape checks, new-order rules (released
  sequence-0 node), duplicate detection, and rejection with `VALIDATION_FAILURE`
  errors in `state`.
- Order updates and stitching per the spec's decision-point rules, including
  `START_NODE_OUT_OF_RANGE` and `ORDER_UPDATE_FOLLOWING_CANCEL` rejections and
  the start-node position check. Append-only updates do not interrupt the move
  in flight.
- Movement execution: drives released nodes with positions one at a time,
  reports traversal via `lastNodeId`, honours `allowedDeviationXY` (0.5 m
  default), settles briefly between consecutive moves, and never dispatches
  through an unlocalized pose.

#### Actions

- `startCleaning` node action: named zones with optional per-zone cleaning-mode
  and settings overrides, blackout bypass; pausable, cancellable, and resumed
  robot-side when the robot resumes its own paused task.
- `startCharging` / `stopCharging` (dock / undock) at INSTANT and NODE scope.
  Docking navigates to the nearest dock; pausable and cancellable. Undock is
  advertised as uncontrollable once started. A move order sent while docked
  undocks automatically first.
- `startPause` / `stopPause` instant actions, routed by what is running
  (movement vs cleaning vs docking).
- `cancelOrder` instant action with optional `orderId` scoping and
  `NO_ORDER_TO_CANCEL` error on an idle robot.
- `initializePosition` instant action (relocalization) on the robot's current
  map, with busy rejection while an order is in flight and the localization
  score reported on failure.

#### Vendor extension

- Retained `cleaningcatalog` topic publishing the active map's cleanable zones,
  cleaning modes, and docks, republished whenever the driver resolves a new map.

#### LionsBot driver

- WebSocket status and pose feeds with token auth, resubscribe, and
  reconnection; REST map metadata polling with cached map frames.
- Frame conversion between VDA world coordinates (metres/radians) and robot map
  pixels, including a configurable theta offset.
- Robot-model to protocol-version mapping for the supported robot families.

#### Configuration & operations

- Configuration via environment variables: `MASTER_*` (fleet-master MQTT
  broker) and `ROBOT_*` (robot connection), with regional endpoint defaults
  derivable from `ROBOT_REGION`. See `.example.env`.
- Structured logging, graceful shutdown on SIGINT/SIGTERM, forced on second
  signal.
- CI workflow (build, vet, test) and a table-driven test suite across all
  layers.

### Not yet implemented

`visualization` topic, `zoneSet`/`zones`, VDA5050 map distribution, cross-map
relocalization, and the `spotClean` action. See the Scope section of the README.

[Unreleased]: https://github.com/lionsbot-official/vda5050-adapter/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/lionsbot-official/vda5050-adapter/releases/tag/v1.0.0
