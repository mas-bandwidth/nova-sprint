"""Render independent ground shadows from a character alpha and planted-toe anchor."""
from PIL import Image, ImageDraw, ImageFilter
import numpy as np

def render_shadow(alpha, size, toe, ground):
    tx, ty = toe
    cx, cy = ground
    # Ground projection: sun from upper left; shadow extends down/right.
    sx, shear, depth = .90, .22, .13
    inverse = (1/sx, -shear/(sx*depth), tx-cx/sx+shear*cy/(sx*depth),
               0, -1/depth, ty+cy/depth)
    projected = alpha.transform(size, Image.Transform.AFFINE, inverse, Image.Resampling.BICUBIC)
    # The sole stays sharp; higher surfaces cast progressively softer shadows.
    yy = np.arange(size[1], dtype=float)[:, None]
    distance = np.clip((yy-cy)/(depth*ty), 0, 1)
    weights = [np.clip(1-distance*2,0,1), 1-abs(distance*2-1), np.clip(distance*2-1,0,1)]
    softness = [1.5, 4.0, 7.0]
    mask = sum(np.asarray(projected.filter(ImageFilter.GaussianBlur(radius)),dtype=float)*weight
               for radius,weight in zip(softness,weights))
    mask *= .56-.16*distance
    cast = Image.new('RGBA',size,(4,14,31,0))
    cast.putalpha(Image.fromarray(np.clip(mask,0,255).astype('uint8')))
    contact = Image.new('RGBA',size)
    d = ImageDraw.Draw(contact)
    d.ellipse((cx-23,cy-2,cx+26,cy+3),fill=(2,8,18,190))
    contact=contact.filter(ImageFilter.GaussianBlur(.8))
    return cast,contact
