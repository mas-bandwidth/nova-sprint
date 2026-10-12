"""Compose the generated plates without regenerating track or typography.
Requires Pillow and NumPy. Run this file from any directory.
"""
from pathlib import Path
import hashlib
import json
import numpy as np
from PIL import Image, ImageDraw, ImageFilter

ROOT = Path(__file__).resolve().parent
background = Image.open(ROOT / 'background.png').convert('RGBA')
source = Image.open(ROOT / 'robot-source.png').convert('RGBA')
alpha = source.getchannel('A')
# Trim transparent padding and very faint generator debris; retain original source.
core = alpha.point(lambda v: 255 if v >= 128 else 0)
bbox = core.getbbox()
source.putalpha(alpha.point(lambda v: v if v >= 32 else 0))
robot = source.crop(bbox)
height = 650
robot = robot.resize((round(robot.width * height / robot.height), height), Image.Resampling.LANCZOS)
a = np.asarray(robot.getchannel('A'))
# Lowest substantial opaque pixels belong to the planted rear shoe toe.
y_rows, x_cols = np.where(a > 160)
toe_y = int(y_rows.max())
contact_xs = np.where(a[toe_y - 2:toe_y + 1].max(axis=0) > 160)[0]
toe_x = float((contact_xs.min() + contact_xs.max()) / 2)
# Measured fixed white boundaries at y=761 and y=802 in the background plate.
lane_top, lane_bottom = 761, 802
ground_y = round((lane_top + lane_bottom) / 2)
target_x = 915
left = round(target_x - toe_x)
top = ground_y - toe_y
cx = left + toe_x
cy = ground_y

# Build independent cast and contact layers, then composite them below the robot.
from render_shadow import render_shadow
cast, contact = render_shadow(robot.getchannel('A'), background.size,
                              (toe_x, toe_y), (cx, cy))
cast.save(ROOT / 'cast-shadow.png')
contact.save(ROOT / 'contact-shadow.png')
shadow = Image.alpha_composite(cast, contact)
placed = Image.new('RGBA', background.size)
placed.alpha_composite(robot, (left, top))
composite = Image.alpha_composite(Image.alpha_composite(background, shadow), placed)
robot.save(ROOT / 'robot-trimmed.png')
placed.save(ROOT / 'robot-placed.png')
shadow.save(ROOT / 'shadow.png')
composite.convert('RGB').save(ROOT / 'composite.png')
# Verify that the background outside the composited layers is byte-for-byte intact.
mask = (np.asarray(shadow.getchannel('A')) > 0) | (np.asarray(placed.getchannel('A')) > 0)
base = np.asarray(background)
out = np.asarray(composite)
assert np.array_equal(base[~mask], out[~mask])
assert lane_top+10 < ground_y < lane_bottom-10
manifest = {
 'lane_boundaries_y': [lane_top, lane_bottom], 'lane_center_y': ground_y,
 'rear_toe_contact': [cx, cy], 'robot_top_left': [left, top],
 'robot_size': list(robot.size), 'unchanged_pixels_outside_layers': True,
 'sources_sha256': {name: hashlib.sha256((ROOT/name).read_bytes()).hexdigest()
                    for name in ['background.png','robot-source.png']},
}
(ROOT / 'placement.json').write_text(json.dumps(manifest, indent=2)+'\n')
print(json.dumps(manifest, indent=2))
