# VDA5050 2.1.0 reference

The English Markdown and schemas are pinned to the official [2.1.0 tag](https://github.com/VDA5050/VDA5050/tree/2.1.0), commit `511d01d71587e8a3dc5e71d4f41dc52466f4284c`. Schema files came from `json_schemas/`. The VDA published PDF has authority over these repository artifacts. Diagrams linked from the upstream Markdown are available in the upstream repository; they are not bundled here.

Documented schema corrections (relative to that tag):

- `state.schema`: include `PAUSED` in `actionStatus`, required by English specification lines 1118 and 1192.
- `factsheet.schema`: move `blockingTypes`' string enum from the array itself to its `items`, matching the string-array definition at line 1408.

The official `agvClass` enum is preserved. This adapter reports the generic `CARRIER` class with `maxLoadMass: 0`; `startCleaning` is a manufacturer action, not a standard AGV class.

Known artifact discrepancies remain visible: `allowedDeviationTheta`'s schema minimum is negative although the text requires [0, Pi]; the adapter does not support supplied angular-tolerance constraints and rejects them. State battery-health/reach schema fields accept numbers while the normative text specifies integers; the Go model retains integers. A complete PDF/Markdown reconciliation and customer-master/hardware acceptance remain release prerequisites.

The package embeds the incoming-message schemas so a built adapter never depends on files relative to its working directory. See `LICENSE.txt` for the VDA copyright and permission notice.

## Final published edition cross-check

The authoritative **January 2025 V2.1.0 English PDF** was retrieved on 2026-10-10 from the [official VDA archive ZIP](https://www.vda.de/dam/jcr:f4043700-24d0-42dc-9315-468f5ab2c10c/VDA5050_PreviousVersions.zip), entry `VDA5050-V2.1.0-2025-01.pdf`. SHA-256: `a056bf2efbd58ef5fe22a10c18776ee27d2674e26a7f18bd9736475d07de74e7`. A bundled copy is `VDA5050_EN.pdf`; it prevails over the tagged Markdown and schemas.

Targeted reconciliation confirms the tagged text for these implemented requirements (printed PDF pages): optional supplied fields must be honored or the order rejected (p.10; Markdown §6.1.1); original stitching-node instructions remain authoritative (p.17); cancellation waits until movement and actions stop, with noninterruptible actions completing normally (p.19); `startPause` stops driving but actions may continue (p.35); required `initPosition` parameters include `lastNodeId` (p.35); completed instant-action cleanup on a new order retains running instant actions (p.46); PAUSED is an action status (pp.50,53); entering or leaving MANUAL clears orders and SERVICE/TEACHIN are outside master control (p.53); retained factsheet and standard `factsheetRequest` (p.57); unlisted optional parameters are unsupported (p.61); `blockingTypes` is an array of the NONE/SOFT/HARD strings (p.62).

No behavioral discrepancy was found in these targeted sections. This does not constitute a complete line-by-line reconciliation or customer-master/hardware acceptance.
