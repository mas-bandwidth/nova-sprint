"""Place the fresh starting-blocks mascot on a grass-free track, losslessly."""
from pathlib import Path
import json
import numpy as np
from PIL import Image, ImageDraw, ImageFilter, ImageChops

ROOT = Path(__file__).resolve().parent
OUT = ROOT / 'starting-blocks'
OUT.mkdir(exist_ok=True)
source = Image.open(ROOT / 'solo-runner-v2/track.png').convert('RGBA')
pixels = np.asarray(source).copy()
h, w = pixels.shape[:2]
# Continue the sky's local color through the narrow grass strip, stopping at
# the blue track edge. Everything below that edge stays byte-for-byte intact.
sky = pixels[430:440, :, :3].mean(axis=0)
for x in range(w):
    edge = 480 + 18 * x / (w - 1)
    for y in range(440, int(edge) + 1):
        opacity = min(1.0, edge - y)
        pixels[y, x, :3] = np.rint(sky[x] * opacity + pixels[y, x, :3] * (1-opacity)).astype(np.uint8)
track = Image.fromarray(pixels)
track.save(OUT / 'track.png')
assert np.array_equal(pixels[499:], np.asarray(source)[499:])

def place(path, width, xy):
    asset = Image.open(path).convert('RGBA')
    asset = asset.crop(asset.getchannel('A').point(lambda a: 255 if a > 8 else 0).getbbox())
    asset = asset.resize((width, round(asset.height * width / asset.width)), Image.Resampling.LANCZOS)
    layer = Image.new('RGBA', track.size)
    layer.alpha_composite(asset, xy)
    return layer

bot = place(OUT / 'character.png', 890, (60, 125))
logo = place(ROOT / 'wordmark.png', 1080, (1020, 202))
mask = Image.new('L', track.size)
draw = ImageDraw.Draw(mask)
draw.ellipse((80, 553, 660, 612), fill=88)
draw.ellipse((695, 583, 950, 630), fill=100)
mask = mask.filter(ImageFilter.GaussianBlur(12))
shadow = Image.new('RGBA', track.size, (6, 22, 46, 0))
shadow.putalpha(mask)
composite = track.copy()
coverage = Image.new('L', track.size)
for name, layer in [('shadows', shadow), ('character-placed', bot), ('wordmark', logo)]:
    layer.save(OUT / (name + '.png'))
    composite = Image.alpha_composite(composite, layer)
    coverage = ImageChops.lighter(coverage, layer.getchannel('A'))
outside = coverage.point(lambda a: 255 if a == 0 else 0).convert('RGB')
assert ImageChops.multiply(ImageChops.difference(composite, track).convert('RGB'), outside).getbbox() is None
composite.save(OUT / 'composite.png')
# Also preserve the approved running arrangement with just its grass removed.
running = track.copy()
for name in ['shadows', 'runner', 'wordmark']:
    running = Image.alpha_composite(running, Image.open(ROOT / 'solo-runner-v2' / (name+'.png')).convert('RGBA'))
running.save(OUT / 'running-no-grass.png')
(OUT / 'layout.json').write_text(json.dumps({
    'canvas': track.size, 'character_width': 890, 'character_position': [60,125],
    'wordmark_width': 1080, 'wordmark_position': [1020,202],
    'grass_removed_by': 'local sky color continuation above track edge',
    'lane_pixels_preserved_below_row': 499
}, indent=2)+'\n')
print(OUT / 'composite.png')
