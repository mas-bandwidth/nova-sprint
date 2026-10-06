"""Build illustrated guide panels from the original lossless character assets.

Requires Pillow. Set NOVA_BRAND_FONT to an Arial-compatible TrueType font if
Arial is not installed at the default macOS location. No generated source is
modified; all panels render at 4x logical resolution and export at 2x for
crisp type and antialiased geometry on high-density displays. Use --theme dark
for dark variants; --panels limits which panels are written. The README's newer
coordination-short-legs artwork is a separate generated asset, never overwritten.
"""
from pathlib import Path
import argparse, os, math
from PIL import Image, ImageDraw, ImageFont
ROOT = Path(__file__).resolve().parent
OUT = ROOT / 'explainer'
OUT.mkdir(exist_ok=True)
FONT = os.environ.get('NOVA_BRAND_FONT', '/System/Library/Fonts/Supplemental/Arial.ttf')
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--theme', choices=('light', 'dark'), default='light')
parser.add_argument('--panels', nargs='+', choices=(
    'coordination', 'machine', 'team', 'streams', 'ready', 'landed'))
args = parser.parse_args()
DARK = args.theme == 'dark'
NAVY = '#f0f6fc' if DARK else '#102957'
BLUE = '#58a6ff' if DARK else '#087ff5'
MUTED = '#aeb8c4' if DARK else '#58677d'
PAPER = '#0d1117' if DARK else '#f8f7f2'
DIVIDER = '#30363d' if DARK else '#dedfdc'
STATE = '#182d49' if DARK else '#e8f1fd'
SUCCESS = '#173c2a' if DARK else '#e4f4eb'
REPAIR = '#bc8cff' if DARK else '#9760c4'
BACKEND = '#152235' if DARK else '#edf3fb'
APP = '#261e35' if DARK else '#f0eafa'
RELEASE = '#182b23' if DARK else '#e9f3ec'
CARD = '#161b22' if DARK else 'white'
BORDER = '#536173' if DARK else '#cbd5e3'
SENTINEL = '#3b2e12' if DARK else '#fff1c9'
SENTINEL_BORDER = '#b68b32' if DARK else '#d9af48'
CHECK_BG = '#173c2a' if DARK else '#dcf1e5'
CHECK = '#56d364' if DARK else '#20804d'

def font(size): return ImageFont.truetype(FONT, size)
class ScaledDraw:
    """Keep layout in logical pixels while rendering geometry and type at 4x."""
    def __init__(self, im, scale):
        self.surface, self.scale = im, scale
        self.draw = ImageDraw.Draw(im)
    def coords(self, value):
        if isinstance(value,(tuple,list)):
            return tuple(self.coords(v) for v in value)
        return value*self.scale
    def __getattr__(self, name):
        def call(*args, **kwargs):
            args=list(args)
            args[0]=self.coords(args[0])
            if name=='rounded_rectangle' and len(args)>1:
                args[1]*=self.scale
            if 'width' in kwargs: kwargs['width']*=self.scale
            if 'font' in kwargs:
                kwargs['font']=kwargs['font'].font_variant(size=kwargs['font'].size*self.scale)
            return getattr(self.draw,name)(*args,**kwargs)
        return call
def canvas(h=490,scale=4):
    im = Image.new('RGBA',(1600*scale,h*scale),PAPER)
    im.info['drawing_scale']=scale
    draw = ScaledDraw(im,scale)
    return im,draw
def text(d,xy,t,size=30,color=NAVY,anchor=None):
    d.text(xy,t,font=font(size),fill=color,anchor=anchor)
def asset(im,name,xy,width):
    scale=im.info['drawing_scale']
    width*=scale
    xy=tuple(v*scale for v in xy)
    a=Image.open(ROOT/name).convert('RGBA')
    a=a.crop(a.getchannel('A').point(lambda v:255 if v>8 else 0).getbbox())
    a=a.resize((width,round(width*a.height/a.width)),Image.Resampling.LANCZOS)
    im.alpha_composite(a,xy)
def arrow(d,points,color=BLUE,width=2.5,head=True):
    """Rounded connectors drawn at 4x resolution, then antialiased once.

    Composite only the connector bounds so the original character and text
    pixels stay untouched. The path stops at the arrowhead base, avoiding a
    blunt stroke protruding through the tip.
    """
    output_scale=d.scale
    scale=4*output_scale
    pad=14
    left=math.floor(min(p[0] for p in points))-pad
    top=math.floor(min(p[1] for p in points))-pad
    right=math.ceil(max(p[0] for p in points))+pad
    bottom=math.ceil(max(p[1] for p in points))+pad
    layer=Image.new('RGBA',((right-left)*scale,(bottom-top)*scale))
    draw=ImageDraw.Draw(layer)
    def xy(p): return ((p[0]-left)*scale,(p[1]-top)*scale)
    tip=points[-1]
    angle=math.atan2(tip[1]-points[-2][1],tip[0]-points[-2][0])
    path=list(points)
    if head:
        path[-1]=(tip[0]-7*math.cos(angle),tip[1]-7*math.sin(angle))
    smooth=[path[0]]
    for a,b,c in zip(path,path[1:],path[2:]):
        ab=math.dist(a,b); bc=math.dist(b,c)
        radius=min(12,ab/2,bc/2)
        entry=(b[0]+(a[0]-b[0])*radius/ab,b[1]+(a[1]-b[1])*radius/ab)
        leave=(b[0]+(c[0]-b[0])*radius/bc,b[1]+(c[1]-b[1])*radius/bc)
        smooth.append(entry)
        for i in range(1,25):
            t=i/24
            smooth.append(tuple((1-t)**2*entry[j]+2*(1-t)*t*b[j]+t*t*leave[j] for j in (0,1)))
    smooth.append(path[-1])
    draw.line([xy(p) for p in smooth],fill=color,width=round(width*scale),joint='curve')
    x,y=xy(smooth[0]); r=width*scale/2
    draw.ellipse((x-r,y-r,x+r,y+r),fill=color)
    if head:
        base=path[-1]; normal=(-math.sin(angle)*3.8,math.cos(angle)*3.8)
        draw.polygon([xy(tip),xy((base[0]+normal[0],base[1]+normal[1])),xy((base[0]-normal[0],base[1]-normal[1]))],fill=color)
    layer=layer.resize(((right-left)*output_scale,(bottom-top)*output_scale),Image.Resampling.LANCZOS)
    d.surface.alpha_composite(layer,(left*output_scale,top*output_scale))
def save(im,name):
    if args.panels and name not in args.panels:
        return
    scale=im.info['drawing_scale']
    if scale>1:
        im=im.resize((im.width*2//scale,im.height*2//scale),Image.Resampling.LANCZOS)
    suffix = '-dark' if DARK else ''
    im.convert('RGB').save(OUT/(name+suffix+'.png'))

im,d=canvas()
for x in [530,1065]: d.line([(x,55),(x,435)],fill=DIVIDER,width=2)
text(d,(265,42),'Still needed. Fast asleep.',32,anchor='mt')
text(d,(797,42),'A dependency goes missing.',32,anchor='mt')
text(d,(1330,42),'Can anybody hear me?',32,anchor='mt')
asset(im,'yellow-v2.png',(45,195),450)
asset(im,'purple.png',(567,110),460)
asset(im,'green.png',(1208,100),250)
text(d,(265,439),'YELLOW',21,MUTED,'mt');text(d,(797,439),'PURPLE',21,MUTED,'mt');text(d,(1330,439),'GREEN',21,MUTED,'mt')
save(im,'coordination')

im,d=canvas(450,scale=4)
asset(im,'stella.png',(45,120),280)
text(d,(185,50),'AI coordinator',33,anchor='mt')
text(d,(185,397),'Plans + decisions',25,MUTED,'mt')
text(d,(985,49),'THE MACHINE KEEPS THE HANDOFFS MOVING',29,anchor='mt')
states=['Waiting','Ready','Working','Review','Merging','Landed']
for i,s in enumerate(states):
    x=430+i*189
    fill=SUCCESS if s=='Landed' else STATE
    d.rounded_rectangle((x,170,x+150,255),18,fill=fill)
    text(d,(x+75,212),s,28,NAVY,'mm')
    if i<5: arrow(d,[(x+158,212),(x+181,212)])
arrow(d,[(1072,266),(1072,322),(883,322),(883,266)],REPAIR)
text(d,(985,355),'Findings + a coordinator decision → another attempt',24,MUTED,'mt')
text(d,(985,402),'Shared state • explicit rules • recorded progress • recovery',24,NAVY,'mt')
save(im,'machine')

im,d=canvas(510)
d.line([(995,55),(995,457)],fill=DIVIDER,width=2)
text(d,(490,35),'A swarm goes wide across the fleet.',34,anchor='mt')
asset(im,'bees.png',(75,119),395)
asset(im,'orange.png',(520,100),350)
text(d,(1280,35),'A friend sets her pace.',34,anchor='mt')
asset(im,'pink-cyclist.png',(1135,98),290)
text(d,(490,468),'MANY BOUNDED TASKS, ONE SHARED SPRINT',22,MUTED,'mt')
text(d,(1280,468),'ROOM FOR DIFFERENT STRENGTHS',22,MUTED,'mt')
save(im,'team')

im,d=canvas(590,scale=4)
text(d,(65,30),'Write the workflow. Let the machine run it.',39)
for label,y,tint in [('BACKEND',100,BACKEND),('APP',245,APP),('RELEASE',390,RELEASE)]:
    d.rounded_rectangle((220,y,1545,y+120),20,fill=tint)
    text(d,(55,y+47),label,25,NAVY)
def card(x,y,label,note,width=255):
    d.rounded_rectangle((x,y,x+width,y+75),12,fill=CARD,outline=BORDER,width=2)
    text(d,(x+width/2,y+13),label,26,NAVY,'mt')
    text(d,(x+width/2,y+45),note,20,MUTED,'mt')
card(255,122,'Agree API contract','Land this first',265)
card(665,122,'Build API','Runs in parallel',250)
card(665,267,'Build client','Runs in parallel',250)
arrow(d,[(528,160),(657,160)])
arrow(d,[(580,160),(580,305),(657,305)])
arrow(d,[(923,160),(975,160),(975,450),(1015,450)])
arrow(d,[(923,305),(975,305)],head=False)
d.rounded_rectangle((1023,410,1265,490),12,fill=SENTINEL,outline=SENTINEL_BORDER,width=2)
text(d,(1144,421),'Sentinel',29,NAVY,'mt')
text(d,(1144,459),'Both landed → release',20,MUTED,'mt')
arrow(d,[(1273,450),(1300,450)])
card(1308,413,'Integration','Check both together',220)
text(d,(800,540),'Rows = work streams • boxes = cards • arrows = dependencies • sentinel = gate',26,MUTED,'mt')
save(im,'streams')

im,d=canvas(470)
asset(im,'starting-blocks/character.png',(55,55),650)
text(d,(900,130),'Ready for the next card.',45)
text(d,(900,212),'Useful work queued.',30,MUTED)
text(d,(900,260),'Dependencies satisfied.',30,MUTED)
text(d,(900,308),'Capacity available. Off we go.',30,MUTED)
save(im,'ready')

im,d=canvas(480)
asset(im,'white.png',(100,38),385)
text(d,(650,68),'Carry the work all the way home.',43)
for i,(label,note) in enumerate([('Implement','A concrete result and commit'),('Review + check','Independent eyes, relevant tests'),('Land','Integrated into the development branch')]):
    y=160+i*87
    d.ellipse((650,y,693,y+43),fill=CHECK_BG)
    d.line([(662,y+23),(670,y+31),(683,y+14)],fill=CHECK,width=4)
    text(d,(719,y-1),label,30)
    text(d,(719,y+36),note,24,MUTED)
save(im,'landed')
print('Saved', len(args.panels or range(6)), args.theme, 'lossless explainer panels to',OUT)
