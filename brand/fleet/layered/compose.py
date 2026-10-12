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

# Project the actual silhouette onto the horizontal track from upper-left light.
sx, shear, depth = .72, .50, .055
inverse = (1/sx, -shear/(sx*depth), toe_x-cx/sx+shear*cy/(sx*depth),
           0, -1/depth, toe_y+cy/depth)
projected = robot.getchannel('A').transform(background.size, Image.Transform.AFFINE,
                                           inverse, Image.Resampling.BICUBIC)
projected = projected.filter(ImageFilter.GaussianBlur(3.5)).point(lambda v: round(v*.27))
shadow = Image.new('RGBA', background.size, (8, 23, 49, 0))
shadow.putalpha(projected)
# Tight grounded contact, exactly beneath the rear shoe's opaque toe edge.
contact = Image.new('RGBA', background.size)
d = ImageDraw.Draw(contact)
d.ellipse((cx-22, cy-3, cx+24, cy+3), fill=(5, 17, 37, 125))
contact = contact.filter(ImageFilter.GaussianBlur(1.15))
shadow = Image.alpha_composite(shadow, contact)
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
