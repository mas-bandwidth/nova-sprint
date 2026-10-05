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
asset(im,'purple.png',(567,110),460)
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

im,d=canvas(590)
text(d,(65,30),'Write the workflow. Let the machine run it.',39)
for label,y,tint in [('BACKEND',100,'#edf3fb'),('APP',245,'#f0eafa'),('RELEASE',390,'#e9f3ec')]:
    d.rounded_rectangle((220,y,1545,y+120),20,fill=tint)
    text(d,(55,y+47),label,25,NAVY)
def card(x,y,label,note,width=255):
    d.rounded_rectangle((x,y,x+width,y+75),12,fill='white',outline='#cbd5e3',width=2)
    text(d,(x+width/2,y+13),label,26,NAVY,'mt')
    text(d,(x+width/2,y+45),note,20,MUTED,'mt')
card(255,122,'Agree API contract','Land this first',265)
card(665,122,'Build API','Runs in parallel',250)
card(665,267,'Build client','Runs in parallel',250)
arrow(d,[(528,160),(657,160)])
arrow(d,[(580,160),(580,305),(657,305)])
d.line([(923,160),(975,160),(975,450)],fill=BLUE,width=4)
d.line([(923,305),(975,305)],fill=BLUE,width=4)
arrow(d,[(975,450),(1015,450)])
d.rounded_rectangle((1023,410,1265,490),12,fill='#fff1c9',outline='#d9af48',width=2)
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
    d.ellipse((650,y,693,y+43),fill='#dcf1e5')
    d.line([(662,y+23),(670,y+31),(683,y+14)],fill='#20804d',width=4)
    text(d,(719,y-1),label,30)
    text(d,(719,y+36),note,24,MUTED)
save(im,'landed')
print('Saved six lossless explainer panels to',OUT)
