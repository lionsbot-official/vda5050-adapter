# VDA5050 2.0.0 reference

The authoritative reference is the bundled **January 2022 final English PDF**, `VDA5050_EN.pdf`, retrieved on 2026-10-10 from the [VDA archive](https://www.vda.de/en/news/publications/publication/vda-5050-archive-of-older-versions), [official ZIP](https://www.vda.de/dam/jcr:f4043700-24d0-42dc-9315-468f5ab2c10c/VDA5050_PreviousVersions.zip), entry `VDA5050-V2_0_0-EN.pdf`. Its SHA-256 is `aabcadac02479a02feade336d3196a5a3aeedf1aa6eca27dc80654dadffa965c`.

The Markdown and original schemas come from the official [2.0.0 tag](https://github.com/VDA5050/VDA5050/tree/2.0.0), commit `a8974b1f8bc412f72cca237f14c5578fe1b01224`. Upstream Markdown is named `VDA5050_EN_V1.md` and retains a release-candidate heading; it is included for searchable context, **not as final authority**. The PDF prevails. In particular, the final PDF defines `factsheetRequest` (pp.31,33,49), retained factsheet publication (p.49), and `orientationType` (p.26), despite omissions in tagged sources.

Schema repairs relative to the tag:

- `order.schema`: correct `allowedDeviationXy` to `allowedDeviationXY` (PDF p.24); add `orientationType` with `GLOBAL`/`TANGENTIAL` (p.26).
- `instantActions.schema`: correct `actionName` to `actionType` (p.24) and require the envelope fields (p.34).
- `state.schema`: include `PAUSED` (pp.43,45); make node-position `theta` optional, as nodeState references the order node-position definition (pp.24,39).
- `factsheet.schema`: wrap misplaced root fields and `maxArrayLens` fields in `properties` (pp.49–53), require `headerId` and `timestamp` (p.49), correct localizationParameters to an object (p.50), correct `PHYSICAL_LINDE_GUIDED` to `PHYSICAL_LINE_GUIDED` (p.50), and repair load description/string, `maxWeight`, and bounding-box angle/number fields (pp.42,57). The upstream filename is `factsheet.json`.

No V2.1 map/corridor/vehicle-configuration fields or factsheet blocking-type metadata are added. Both incoming action-parameter schemas include object values: the final PDF lists `OBJECT` among parameter data types (p.54), despite the tagged schemas omitting it. The cleaning extension uses an array of zone objects. Unsupported constraints are rejected before execution.

The official AGV-class enum is preserved. `CARRIER` with zero load capacity is the current adapter profile; V2.0 defines no cleaning class. This choice and the pinned physical parameters require integrator confirmation before deployment and are not evidence of conformance. Hardware/customer-master acceptance remains outstanding.

The package embeds incoming-message schemas; binaries do not depend on files relative to their working directory. See `LICENSE.txt` for the upstream repository notice; the final PDF retains its original VDA copyright notice. Its p.60 notice permits reprint when the source is stated; the VDA publisher, edition, source URL and checksum are attributed above.
