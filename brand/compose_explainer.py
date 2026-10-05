"""Build illustrated guide panels from the original lossless character assets.

Requires Pillow. Set NOVA_BRAND_FONT to an Arial-compatible TrueType font if
Arial is not installed at the default macOS location. No generated source is
modified; the diagrams and type are drawn deterministically.
"""
from pathlib import Path
import os, math
from PIL import Image, ImageDraw, ImageFont
ROOT = Path(__file__).resolve().parent
OUT = ROOT / 'explainer'
OUT.mkdir(exist_ok=True)
FONT = os.environ.get('NOVA_BRAND_FONT', '/System/Library/Fonts/Supplemental/Arial.ttf')
NAVY = '#102957'
BLUE = '#087ff5'
MUTED = '#58677d'
PAPER = '#f8f7f2'

def font(size): return ImageFont.truetype(FONT, size)
def canvas(h=490):
    im = Image.new('RGBA',(1600,h),PAPER)
    return im,ImageDraw.Draw(im)
def text(d,xy,t,size=30,color=NAVY,anchor=None):
    d.text(xy,t,font=font(size),fill=color,anchor=anchor)
def asset(im,name,xy,width):
    a=Image.open(ROOT/name).convert('RGBA')
    a=a.crop(a.getchannel('A').point(lambda v:255 if v>8 else 0).getbbox())
    a=a.resize((width,round(width*a.height/a.width)),Image.Resampling.LANCZOS)
    im.alpha_composite(a,xy)
def arrow(d,points,color=BLUE,width=4):
    d.line(points,fill=color,width=width,joint='curve')
    x,y=points[-1]; px,py=points[-2]; a=math.atan2(y-py,x-px)
    d.polygon([(x,y),(x-13*math.cos(a-.5),y-13*math.sin(a-.5)),(x-13*math.cos(a+.5),y-13*math.sin(a+.5))],fill=color)
def save(im,name): im.convert('RGB').save(OUT/(name+'.png'))

im,d=canvas()
for x in [530,1065]: d.line([(x,55),(x,435)],fill='#dedfdc',width=2)
text(d,(265,42),'Still needed. Fast asleep.',32,anchor='mt')
text(d,(797,42),'A dependency goes missing.',32,anchor='mt')
text(d,(1330,42),'Can anybody hear me?',32,anchor='mt')
asset(im,'yellow.png',(45,195),450)
asset(im,'purple.png',(567,163),460)
asset(im,'green.png',(1190,110),250)
text(d,(265,439),'YELLOW',21,MUTED,'mt');text(d,(797,439),'PURPLE',21,MUTED,'mt');text(d,(1330,439),'GREEN',21,MUTED,'mt')
save(im,'coordination')

im,d=canvas(450)
asset(im,'stella.png',(45,120),280)
text(d,(185,50),'AI coordinator',33,anchor='mt')
text(d,(185,397),'Plans + decisions',25,MUTED,'mt')
text(d,(985,49),'THE MACHINE KEEPS THE HANDOFFS MOVING',29,anchor='mt')
states=['Waiting','Ready','Working','Review','Merging','Landed']
for i,s in enumerate(states):
    x=430+i*189
    fill='#e4f4eb' if s=='Landed' else '#e8f1fd'
    d.rounded_rectangle((x,170,x+164,255),18,fill=fill)
    text(d,(x+82,212),s,28,NAVY,'mm')
    if i<5: arrow(d,[(x+167,212),(x+185,212)])
arrow(d,[(1079,263),(1079,322),(890,322),(890,263)],'#9760c4')
text(d,(985,355),'Findings + a coordinator decision → another attempt',24,MUTED,'mt')
text(d,(985,402),'Shared state • explicit rules • recorded progress • recovery',24,NAVY,'mt')
save(im,'machine')

im,d=canvas(510)
d.line([(995,55),(995,457)],fill='#dedfdc',width=2)
text(d,(490,35),'A swarm goes wide across the fleet.',34,anchor='mt')
asset(im,'bees.png',(75,119),395)
asset(im,'orange.png',(520,100),350)
text(d,(1280,35),'A friend sets her pace.',34,anchor='mt')
asset(im,'pink-cyclist.png',(1135,98),290)
text(d,(490,468),'MANY BOUNDED TASKS, ONE SHARED SPRINT',22,MUTED,'mt')
text(d,(1280,468),'ROOM FOR DIFFERENT STRENGTHS',22,MUTED,'mt')
save(im,'team')

im,d=canvas(490)
text(d,(65,35),'One goal, several streams of work',39)
text(d,(65,106),'SEARCH',23,BLUE)
text(d,(65,370),'DOCS',23,'#9861c5')
d.rounded_rectangle((245,110,1535,335),20,fill='#edf3fb')
d.rounded_rectangle((245,362,1535,455),20,fill='#f0eafa')
def card(x,y,label,note,width=255):
    d.rounded_rectangle((x,y,x+width,y+75),12,fill='white',outline='#cbd5e3',width=2)
    text(d,(x+width/2,y+13),label,26,NAVY,'mt')
    text(d,(x+width/2,y+45),note,20,MUTED,'mt')
card(290,184,'Agree the API','Land this first')
card(710,130,'Build the index','Independent work')
card(710,244,'Build the interface','Independent work')
card(1175,184,'Integration checks','Needs both changes')
arrow(d,[(550,222),(625,222),(625,167),(702,167)])
arrow(d,[(625,222),(625,282),(702,282)])
arrow(d,[(973,167),(1060,167),(1060,222),(1167,222)])
arrow(d,[(973,282),(1060,282),(1060,222)])
card(290,371,'Draft the guide','Can progress alongside',340)
text(d,(1120,405),'Each box is a card. Arrows are dependencies.',25,MUTED,'mm')
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
    d.ellipse((650,y,693,y+43),fill='#dcf1e5')
    d.line([(662,y+23),(670,y+31),(683,y+14)],fill='#20804d',width=4)
    text(d,(719,y-1),label,30)
    text(d,(719,y+36),note,24,MUTED)
save(im,'landed')
print('Saved six lossless explainer panels to',OUT)
