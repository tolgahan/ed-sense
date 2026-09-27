"""Palette-based HUD reader reference (any HUD colours). Go port: hud.go."""
import numpy as np, math
import ref2
from ref2 import TH, text_candidates, rectify, read_cells, label8, value_of
DEFAULT_PALETTE=dict(shield=(41,200,207),heat=(186,108,22),flame=(147,30,27),flash=(56,98,193))
TOL_SHIELD,TOL_HEAT,TOL_FLAME,VREL_FLAME,TOL_FLASH,VREL_FLASH=0.5,0.4,0.35,0.6,0.35,0.66
TOL_TEXT=TOL_SHIELD
def chroma_img(bgr):
    rgb=bgr[...,::-1].astype(np.float32)/255
    mx=rgb.max(-1)
    return rgb/np.maximum(mx,1e-6)[...,None], mx
def chroma(rgb):
    rgb=np.asarray(rgb,np.float32)/255; return rgb/max(rgb.max(),1e-6), float(rgb.max())
def score(bgr,target,tol=TOL_SHIELD):
    c,v=chroma_img(bgr); ct,vt=chroma(target)
    d=np.sqrt(((c-ct)**2).sum(-1))
    vmin=0.3*vt
    return (np.clip(1-d/tol,0,1)*np.clip((v-vmin)/(0.4*vt),0,1)).astype(np.float32)
def mask(bgr,target,tol,vrel):
    c,v=chroma_img(bgr); ct,vt=chroma(target)
    d=np.sqrt(((c-ct)**2).sum(-1))
    return (d<tol)&(v>vrel*vt)
def flames(m,H): return ref2.flames(m,H)
def read_shield(bgr,H,T,pal):
    s=score(bgr,pal['shield']); best=None
    for c in text_candidates(s,H):
        p=rectify(s,c)
        if p is None: continue
        r=read_cells(p,T,'shield')
        if r and (best is None or r[1]>best[1]): best=(r[0],r[1],c)
    return best
def icon_left(bgr,c,text_rgb):
    """a bright blob of another colour just left of the text (the heat flame)"""
    gh=c['gh']; x0=int(c['x']-2.2*gh); x1=int(c['x']-0.05*gh); y0=int(c['y']-0.4*gh); y1=int(c['y']+c['h']+0.4*gh)
    if x0<0 or y0<0 or y1>bgr.shape[0] or x1<=x0: return False
    reg=bgr[y0:y1,x0:x1]
    cc,v=chroma_img(reg); ct,vt=chroma(text_rgb)
    d=np.sqrt(((cc-ct)**2).sum(-1))
    m=(v>0.35)&(d>0.35)
    n=int(m.sum())
    if n<0.2*gh*gh or n>2.0*gh*gh: return False
    ys,xs=np.nonzero(m)
    hgt=ys.max()-ys.min()+1
    return 0.5*gh<=hgt<=2.0*gh
def read_heat(bgr,H,T,pal):
    s=score(bgr,pal['heat'],TOL_HEAT); best=None
    # 1) flame first (fast, and safest where the flame colour is clear)
    for f in flames(mask(bgr,pal['flame'],TOL_FLAME,VREL_FLAME),H):
        h=f['h']; bx0=int(f['x']+f['w']); by0=int(f['y']-1.3*h); bx1=int(bx0+6.5*h); by1=int(f['y']+1.1*h)
        if by0<0: continue
        sub=s[by0:by1,bx0:bx1]
        if sub.shape[0]<8 or sub.shape[1]<8: continue
        for c in text_candidates(sub,H):
            if c['x']<1.8*h and 0.6*h<=c['gh']<=1.4*h:
                p=rectify(sub,c)
                if p is None: continue
                r=read_cells(p,T,'heat')
                if r and (best is None or r[1]>best[1]): best=(r[0],r[1],dict(c,x=c['x']+bx0,y=c['y']+by0),p)
    if best: return best
    # 2) text first: any heat-coloured "NN%" with an icon of another colour on its left
    for c in text_candidates(s,H):
        if c['n']>5 or not icon_left(bgr,c,pal['heat']): continue
        p=rectify(s,c)
        if p is None: continue
        r=read_cells(p,T,'heat')
        if r and (best is None or r[1]>best[1]): best=(r[0],r[1],c,p)
    return best
def splash(bgr_roi,c,pal):
    tx=c['x']+c['w']/2; ty=c['y']+c['h']/2; g=c['gh']
    Hh,Ww=bgr_roi.shape[:2]
    x0,x1=max(0,int(tx-11*g)),min(Ww,int(tx+11*g)); y0,y1=max(0,int(ty-11*g)),min(Hh,int(ty+0.5*g))
    if x1<=x0 or y1<=y0: return 0.0,0.0
    b=mask(bgr_roi[y0:y1,x0:x1],pal['flash'],TOL_FLASH,VREL_FLASH)
    n=int(b.sum())
    if n==0: return 0.0,0.0
    xs=np.nonzero(b)[1]+x0
    return n/(g*g), float((xs.mean()-tx)/(6*g))
# ---------------- calibration: find the text colours on an unknown HUD ----------------
def candidate_colours(bgr,k=6):
    """dominant bright colours: 24 hue bins over saturated pixels + one bin for white/grey.
    Each bin's colour is taken from its most saturated, brightest pixels (the core of text strokes)."""
    rgb=bgr[...,::-1].astype(np.float32)/255
    mx=rgb.max(-1); mn=rgb.min(-1); sat=(mx-mn)/np.maximum(mx,1e-6)
    sel=mx>0.35
    h,s,v=ref2.hsv_arrays(bgr)
    bins=np.where(sat>0.25,(h/15).astype(int)%24,24)
    out=[]
    for b in range(25):
        m=sel&(bins==b)
        n=int(m.sum())
        if n<50: continue
        px=rgb[m]; sv=sat[m]*mx[m]
        core=px[sv>=np.percentile(sv,70)] if b<24 else px[mx[m]>=np.percentile(mx[m],70)]
        col=np.median(core,0)*255
        out.append((n,tuple(int(x) for x in col)))
    out.sort(reverse=True)
    return [c for n,c in out[:k]]
def calibrate_shield(bgr_roi,H,T):
    best=None
    for col in candidate_colours(bgr_roi):
        r=read_shield(bgr_roi,H,T,dict(DEFAULT_PALETTE,shield=col))
        if r and value_of(r[0]) is not None and (best is None or r[1]>best[1]): best=(r[0],r[1],col)
    return best
def calibrate_heat(bgr_roi,H,T,shield_col=None):
    best=None
    for col in candidate_colours(bgr_roi):
        if shield_col is not None and np.linalg.norm(chroma(col)[0]-chroma(shield_col)[0])<0.25: continue  # not the target's shield text
        pal=dict(DEFAULT_PALETTE,heat=col,flame=(1,1,1))  # unknown flame: text-first path only
        r=read_heat(bgr_roi,H,T,pal)
        if r and value_of(r[0]) is not None and (best is None or r[1]>best[1]): best=(r[0],r[1],col)
    return best

def main_colour(bgr):
    """the HUD's main colour: the most common bright colour (radar, panels, text)"""
    cs=candidate_colours(bgr,1)
    return cs[0] if cs else None
def calibrate(bgr_shield_roi,bgr_heat_roi,H,T):
    main=main_colour(bgr_heat_roi)
    best=None
    for col in candidate_colours(bgr_shield_roi,8):
        if main is not None and np.linalg.norm(chroma(col)[0]-chroma(main)[0])<0.25: continue
        r=read_shield(bgr_shield_roi,H,T['shield'],dict(DEFAULT_PALETTE,shield=col))
        if r and value_of(r[0]) is not None and (best is None or r[1]>best[1]): best=(r[0],r[1],col)
    pal=dict(DEFAULT_PALETTE)
    if main is not None: pal['heat']=main
    if best: pal['shield']=best[2]
    # flame: the reddest-looking small blobs are tried first, then text-first
    rh=None
    for fc in candidate_colours(bgr_heat_roi,8):
        if np.linalg.norm(chroma(fc)[0]-chroma(pal['heat'])[0])<0.15: continue
        r=read_heat(bgr_heat_roi,H,T['heat'],dict(pal,flame=fc))
        if r and value_of(r[0]) is not None and (rh is None or r[1]>rh[1]): rh=(r[0],r[1],fc)
    if rh: pal['flame']=rh[2]
    return pal,best,rh

def all_reads(bgr,H,T,col,mode):
    """every "N%" text of colour col in bgr: (value_str, score, cx, cy, gh)"""
    s=score(bgr,col,TOL_SHIELD); out=[]
    for c in text_candidates(s,H):
        p=rectify(s,c)
        if p is None: continue
        r=read_cells(p,T,mode)
        if r and value_of(r[0]) is not None and r[1]>=0.7:
            out.append((r[0],r[1],c['x']+c['w']/2,c['y']+c['h']/2,c['gh'],c))
    return out
def calibrate2(bgr,H,W,T):
    """bgr: full frame. Finds shield / HUD colours from the panel layout:
    on the right panel the shield % sits up and to the right of the hull %,
    which has the HUD's main colour (the same as the heat %)."""
    x0,y0=int(W*0.5),int(H*0.5); right=bgr[y0:int(H*0.97), x0:int(W*0.97)]
    cols=candidate_colours(right,8)
    reads={}
    for col in cols:
        rr=all_reads(right,H,T['shield'],col,'shield')
        if rr: reads[col]=rr
    best=None
    for cs,rs in reads.items():
        for cm,rm in reads.items():
            if cs==cm or np.linalg.norm(chroma(cs)[0]-chroma(cm)[0])<0.2: continue
            for a in rs:        # shield candidate
                for b in rm:    # hull candidate
                    g=(a[4]+b[4])/2
                    dx=(a[2]-b[2])/g; dy=(a[3]-b[3])/g
                    if 3<=dx<=16 and -10<=dy<=-2:
                        sc=a[1]+b[1]
                        if best is None or sc>best[0]: best=(sc,cs,cm,a,b)
    return best
