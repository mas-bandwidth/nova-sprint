# Layered fleet banner

Working candidate, October 11, 2026; not a claim of final approval.

- `background.png`: clean scene, generated from the current banner with the robot and its shadow removed. Keeps the logo, copy, starting blocks and horizontal track lanes.
- `robot-source.png`: separate transparent robot generated from the same reference, with a rear-foot push-off pose, white/blue shell, orange shoes and two diagonal cyan eye strokes. Original lossless output.
- `robot-trimmed.png`: trimmed and scaled character used by the compositor.
- `robot-placed.png`: character on a full-size transparent canvas.
- `shadow.png`: projected silhouette and tight rear-toe contact shadow, on transparency.
- `composite.png`: finished lossless composite.
- `placement.json`: exact placement, source hashes and background preservation check.

Both source layers were generated with the built-in OpenAI image tool. Glenn
explicitly requested separate rendering and deterministic compositing after
whole-scene image editing repeatedly failed to align the foot with its lane.
The source reference was the family-style banner with the robot driving off
its rear foot. The background generation removed only robot/shadow; the robot
generation requested only the isolated character without ground or shadow.

Run `python3 brand/fleet/layered/compose.py` from the repository root with Pillow
and NumPy installed. The script anchors the rear toe at y=782, between measured
white lane boundaries y=761 and y=802. This matches the starting block rail's
track depth. The shadow is generated from the actual character silhouette,
with tight toe contact. The script verifies that pixels outside the robot and
shadow layers are identical to the clean background. No generation step is
applied to the assembled composite.
