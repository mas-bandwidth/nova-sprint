"""Composite original PNG assets onto the quiet, side-on track plate."""
from pathlib import Path
from PIL import Image, ImageDraw, ImageFilter, ImageChops
import json

ROOT = Path(__file__).resolve().parent
OUT = ROOT / 'solo-runner-v2'
OUT.mkdir(exist_ok=True)
track = Image.open(OUT / 'track.png').convert('RGBA')

def place(source, width, xy):
    cutout = Image.open(ROOT / source).convert('RGBA')
    bounds = cutout.getchannel('A').point(lambda a: 255 if a > 8 else 0).getbbox()
    cutout = cutout.crop(bounds)
    cutout = cutout.resize((width, round(cutout.height * width / cutout.width)), Image.Resampling.LANCZOS)
    layer = Image.new('RGBA', track.size)
    layer.alpha_composite(cutout, xy)
    return layer

runner = place('white.png', 545, (150, 60))
logo = place('wordmark.png', 1160, (850, 180))
mask = Image.new('L', track.size)
ImageDraw.Draw(mask).ellipse((190, 615, 700, 662), fill=86)
mask = mask.filter(ImageFilter.GaussianBlur(14))
shadow = Image.new('RGBA', track.size, (8, 26, 51, 0))
shadow.putalpha(mask)
composite = track.copy()
coverage = Image.new('L', track.size)
for name, layer in [('shadows', shadow), ('runner', runner), ('wordmark', logo)]:
    layer.save(OUT / (name + '.png'))
    composite = Image.alpha_composite(composite, layer)
    coverage = ImageChops.lighter(coverage, layer.getchannel('A'))
outside = coverage.point(lambda a: 255 if a == 0 else 0).convert('RGB')
assert ImageChops.multiply(ImageChops.difference(composite, track).convert('RGB'), outside).getbbox() is None
composite.save(OUT / 'composite.png')
(OUT / 'layout.json').write_text(json.dumps({
    'canvas': track.size, 'runner_position': [150, 60], 'runner_width': 545,
    'logo_position': [850, 180], 'logo_width': 1160,
    'sources': ['../white.png', '../wordmark.png', 'track.png'],
    'background_pixels_unchanged_outside_layers': True
}, indent=2) + '\n')
print(OUT / 'composite.png')
