"""Rebuild the PNG banner from independent original layers; requires Pillow/NumPy.

Run: python3 brand/compose.py
No generated source is overwritten. Track pixels are only alpha-composited.
"""
from pathlib import Path
from PIL import Image, ImageDraw, ImageFilter, ImageChops
import numpy as np
import hashlib
import json

ROOT = Path(__file__).resolve().parent
background = Image.open(ROOT / 'track.png').convert('RGBA')
W, H = background.size
sx, sy = W / 2172, H / 724
layers = []
layout = {}

# A single projective grid replaces the generated finish strip. The original
# stadium PNG stays intact; only this separate overlay covers its faulty checks.
corners = [(2133,481), (2249,485), (2037,724), (1752,724)]
unit = [(0,0),(1,0),(1,1),(0,1)]
matrix, values = [], []
for (u,v),(x,y) in zip(unit,corners):
    matrix += [[u,v,1,0,0,0,-u*x,-v*x], [0,0,0,u,v,1,-u*y,-v*y]]
    values += [x,y]
homography = np.append(np.linalg.solve(np.array(matrix),np.array(values)),1).reshape(3,3)
AA=4
def projected(u,v):
    p=homography@np.array([u,v,1])
    return (p[0]/p[2]*sx*AA,p[1]/p[2]*sy*AA)
finish_large=Image.new('RGBA',(W*AA,H*AA))
pen=ImageDraw.Draw(finish_large)
rows,cols=12,2
for row in range(rows):
    for col in range(cols):
        color=(242,244,242,255) if (row+col)%2==0 else (43,49,57,255)
        polygon=[projected(col/cols,row/rows),projected((col+1)/cols,row/rows),projected((col+1)/cols,(row+1)/rows),projected(col/cols,(row+1)/rows)]
        pen.polygon(polygon,fill=color)
for u in [0,1]:
    pen.line([projected(u,0),projected(u,1)],fill=(242,244,242,255),width=round(3*sx*AA))
finish=finish_large.resize((W,H),Image.Resampling.LANCZOS)
finish.save(ROOT/'finish-line.png')
clean_track=Image.alpha_composite(background,finish)
clean_track.save(ROOT/'track-clean.png')

def place(name, x, y, *, width=None, height=None):
    original = Image.open(ROOT / (name + '.png')).convert('RGBA')
    alpha = original.getchannel('A')
    assert alpha.getextrema()[0] == 0, f'{name}: genuine transparency required'
    bbox = alpha.point(lambda a: 255 if a > 8 else 0).getbbox()
    # Trim transparent margins only; preserve the original source file.
    obj = original.crop(bbox)
    factor = width * sx / obj.width if width else height * sy / obj.height
    obj = obj.resize((round(obj.width * factor), round(obj.height * factor)), Image.Resampling.LANCZOS)
    pos = (round(x * sx), round(y * sy))
    layer = Image.new('RGBA', background.size)
    layer.alpha_composite(obj, pos)
    layout[name] = {'source_crop': bbox, 'position': pos, 'size': obj.size}
    layers.append((name, layer))

place('wordmark', 636, 22, width=900)
place('stella', 85, 330, width=190)
place('green', 390, 220, height=290)
place('bees', 645, 235, width=215)
place('orange', 860, 220, height=305)
place('pink-cyclist', 1405, 255, height=365)
place('yellow', 320, 480, width=600)
place('purple', 885, 365, width=530)
place('white', 1710, 265, height=425)

# Separate contact and soft cast shadows; no painting or resampling of track.
shadow_alpha = Image.new('L', background.size)
def shadow(box, opacity, blur):
    global shadow_alpha
    mask = Image.new('L', background.size)
    ImageDraw.Draw(mask).ellipse(tuple(round(v * (sx if i % 2 == 0 else sy)) for i,v in enumerate(box)), fill=opacity)
    mask = mask.filter(ImageFilter.GaussianBlur(blur * sx))
    shadow_alpha = ImageChops.lighter(shadow_alpha, mask)

for box, opacity, blur in [
    ((65,478,283,514),82,10),
    ((363,482,619,522),72,12),
    ((838,508,1168,553),60,15),
    ((305,645,938,705),105,11),
    ((907,638,1408,691),63,16),
    ((1372,587,1714,638),62,13),
    ((1694,653,2125,711),67,15),
    ((87,490,266,507),118,3),
    ((407,498,561,514),100,4),
    ((1432,584,1514,606),111,3),
    ((1594,608,1696,626),111,3),
    ((1980,680,2096,701),113,4),
]:
    shadow(box,opacity,blur)
shadows = Image.new('RGBA', background.size, (7,15,39,0))
shadows.putalpha(shadow_alpha)
shadows.save(ROOT / 'shadows.png')

result = Image.alpha_composite(clean_track, shadows)
coverage = ImageChops.lighter(shadow_alpha,finish.getchannel('A'))
for name, layer in layers:
    result = Image.alpha_composite(result, layer)
    coverage = ImageChops.lighter(coverage,layer.getchannel('A'))
result.save(ROOT / 'composite.png')

# Proof: outside all overlays, the final background must match byte for byte.
diff = ImageChops.difference(result.convert('RGB'), background.convert('RGB'))
outside = coverage.point(lambda a: 255 if a == 0 else 0)
assert ImageChops.multiply(diff,Image.merge('RGB',(outside,outside,outside))).getbbox() is None
names=['track','track-clean','finish-line','wordmark','stella','green','orange','yellow','purple','white','pink-cyclist','bees','shadows','composite']
manifest={'canvas':[W,H], 'format':'lossless PNG', 'unexpected_background_changes':0, 'layout':layout, 'sha256':{name+'.png':hashlib.sha256((ROOT/(name+'.png')).read_bytes()).hexdigest() for name in names}}
(ROOT/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
print(json.dumps({'output':str(ROOT/'composite.png'),'canvas':[W,H],'unexpected_background_changes':0}))
