"""Compose a solo-runner banner using the untouched PNG character and wordmark."""
from pathlib import Path
from PIL import Image, ImageDraw, ImageFilter, ImageChops
import json

ROOT=Path(__file__).resolve().parent
OUT=ROOT/'solo-runner'
OUT.mkdir(exist_ok=True)
# Native-resolution 3:1 crop excludes the old finish strip entirely.
track=Image.open(ROOT/'track.png').convert('RGBA').crop((0,0,1872,624))
track.save(OUT/'track.png')

def cutout(name,width=None,height=None):
    image=Image.open(ROOT/name).convert('RGBA')
    bounds=image.getchannel('A').point(lambda a:255 if a>8 else 0).getbbox()
    image=image.crop(bounds)
    factor=height/image.height if height else width/image.width
    return image.resize((round(image.width*factor),round(image.height*factor)),Image.Resampling.LANCZOS)

runner=Image.new('RGBA',track.size)
runner.alpha_composite(cutout('white.png',height=548),(95,34))
runner.save(OUT/'runner.png')
shadowmask=Image.new('L',track.size)
pen=ImageDraw.Draw(shadowmask)
pen.ellipse((95,555,616,603),fill=95)
shadowmask=shadowmask.filter(ImageFilter.GaussianBlur(14))
contact=Image.new('L',track.size)
ImageDraw.Draw(contact).ellipse((446,573,594,593),fill=115)
shadowmask=ImageChops.lighter(shadowmask,contact.filter(ImageFilter.GaussianBlur(4)))
shadow=Image.new('RGBA',track.size,(6,16,38,0)); shadow.putalpha(shadowmask)
shadow.save(OUT/'shadows.png')
base=Image.alpha_composite(track,shadow)

# Simple editorial lockup: the wordmark is clear of the runner's forward hand.
logo=Image.new('RGBA',track.size)
logo.alpha_composite(cutout('wordmark.png',width=1020),(740,125))
logo.save(OUT/'wordmark.png')
clean=Image.alpha_composite(Image.alpha_composite(base,logo),runner)
mask=ImageChops.lighter(shadow.getchannel('A'),logo.getchannel('A'))
mask=ImageChops.lighter(mask,runner.getchannel('A'))
outside=mask.point(lambda a: 255 if a == 0 else 0)
assert ImageChops.multiply(ImageChops.difference(clean,track).convert('RGB'),outside.convert('RGB')).getbbox() is None
clean.save(OUT/'composite.png')

# A second literal race-banner treatment uses a separate blank fabric layer.
ribbon=Image.new('RGBA',track.size)
d=ImageDraw.Draw(ribbon)
d.polygon([(730,255),(1805,281),(1805,483),(730,457)],fill=(248,249,244,249))
d.line([(730,255),(1805,281)],fill=(255,255,255,255),width=3)
d.line([(730,457),(1805,483)],fill=(205,216,217,255),width=2)
ribbon.save(OUT/'ribbon.png')
ribbonmark=Image.new('RGBA',track.size)
ribbonmark.alpha_composite(cutout('wordmark.png',width=910),(810,277))
ribbonmark.save(OUT/'ribbon-wordmark.png')
literal=base.copy()
for layer in [ribbon,ribbonmark,runner]: literal=Image.alpha_composite(literal,layer)
literal.save(OUT/'ribbon-composite.png')
(OUT/'layout.json').write_text(json.dumps({'canvas':track.size,'background_crop':[0,0,1872,624],'runner_position':[95,34],'runner_height':548,'logo_position':[740,125],'logo_width':1020,'source_character':'../white.png','source_wordmark':'../wordmark.png'},indent=2)+'\n')
print(OUT/'composite.png')
