"""Pure-numpy reference HUD reader. The Go reader (internal/hud) started as a port of it."""
import numpy as np, math
TH,CW,CH=20,12,20
def hsv_arrays(bgr):
    im=bgr.astype(np.float32)/255
    b,g,r=im[...,0],im[...,1],im[...,2]
    mx=np.maximum(np.maximum(r,g),b); mn=np.minimum(np.minimum(r,g),b); d=mx-mn
    h=np.zeros_like(mx); nz=d>1e-6
    rm=nz&(mx==r); gm=nz&(mx==g)&~rm; bm=nz&~rm&~gm
    h[rm]=np.mod((g-b)[rm]/d[rm],6); h[gm]=(b-r)[gm]/d[gm]+2; h[bm]=(r-g)[bm]/d[bm]+4
    h*=60
    s=np.where(mx>1e-6,d/np.maximum(mx,1e-6),0)
    return h,s,mx
def hue_score(h,s,v,center,width,smin,vmin):
    dh=np.abs(h-center); dh=np.minimum(dh,360-dh)
    return (np.clip(1-dh/width,0,1)*np.clip((s-smin)/0.3,0,1)*np.clip((v-vmin)/0.3,0,1)).astype(np.float32)
def cyan(bgr): h,s,v=hsv_arrays(bgr); return hue_score(h,s,v,185,25,0.2,0.25)
def orange(bgr): h,s,v=hsv_arrays(bgr); return hue_score(h,s,v,30,18,0.35,0.3)
def red(bgr):
    h,s,v=hsv_arrays(bgr); dh=np.minimum(h,360-h); return (dh<14)&(s>0.55)&(v>0.45)
def blue(bgr):
    h,s,v=hsv_arrays(bgr); return (h>205)&(h<250)&(s>0.45)&(v>0.5)
def label8(mask):
    """two-pass 8-connected labelling; returns (labels, comps) with comps in order of first pixel (raster)"""
    Hh,Ww=mask.shape
    lab=np.zeros((Hh,Ww),np.int32); parent=[0]
    def find(a):
        while parent[a]!=a:
            parent[a]=parent[parent[a]]; a=parent[a]
        return a
    nxt=1
    for y in range(Hh):
        row=mask[y]
        if not row.any(): continue
        for x in np.nonzero(row)[0]:
            nb=[]
            if x>0 and lab[y,x-1]: nb.append(lab[y,x-1])
            if y>0:
                for dx in (-1,0,1):
                    xx=x+dx
                    if 0<=xx<Ww and lab[y-1,xx]: nb.append(lab[y-1,xx])
            if not nb:
                lab[y,x]=nxt; parent.append(nxt); nxt+=1
            else:
                m=min(find(a) for a in nb); lab[y,x]=m
                for a in nb:
                    ra=find(a)
                    if ra!=m: parent[ra]=m
    # resolve and renumber by first appearance
    remap={}; comps=[]
    for y in range(Hh):
        for x in np.nonzero(lab[y])[0]:
            r=find(lab[y,x])
            if r not in remap:
                remap[r]=len(comps)+1; comps.append([x,y,x,y,0])
            k=remap[r]; lab[y,x]=k; c=comps[k-1]
            c[0]=min(c[0],x); c[1]=min(c[1],y); c[2]=max(c[2],x); c[3]=max(c[3],y); c[4]+=1
    out=[dict(id=i+1,x=c[0],y=c[1],w=c[2]-c[0]+1,h=c[3]-c[1]+1,a=c[4]) for i,c in enumerate(comps)]
    return lab,out
def dilate1(m):
    o=m.copy(); o[1:,:]|=m[:-1,:]; o[:-1,:]|=m[1:,:]; o[:,1:]|=m[:,:-1]; o[:,:-1]|=m[:,1:]; return o
def text_candidates(score,H,thr=0.3):
    lab,comps=label8(score>thr)
    comps=[c for c in comps if 0.006*H<=c['h']<=0.022*H and c['w']<=4*c['h'] and c['a']>=0.22*c['w']*c['h']]
    for c in comps: c['cx']=c['x']+c['w']/2; c['cy']=c['y']+c['h']/2
    comps.sort(key=lambda c:c['cx'])
    used=[False]*len(comps); out=[]
    for i,c in enumerate(comps):
        if used[i]: continue
        ch=[i]; last=c
        for j in range(i+1,len(comps)):
            d=comps[j]
            if used[j]: continue
            hm=(last['h']+d['h'])/2
            if d['x']-(last['x']+last['w'])>0.9*hm:
                if d['cx']-last['cx']>2.5*hm: break
                continue
            ov=min(d['y']+d['h'],last['y']+last['h'])-max(d['y'],last['y'])
            if abs(d['cy']-last['cy'])<0.4*hm and 0.65<d['h']/last['h']<1.55 and ov>=0.55*min(d['h'],last['h']):
                ch.append(j); last=d
        cs=[comps[k] for k in ch]
        x0=min(c['x'] for c in cs); x1=max(c['x']+c['w'] for c in cs)
        y0=min(c['y'] for c in cs); y1=max(c['y']+c['h'] for c in cs)
        hs=sorted(c['h'] for c in cs); gh=float(hs[len(hs)//2]) if len(hs)%2 else (hs[len(hs)//2-1]+hs[len(hs)//2])/2
        if 1.3<=(x1-x0)/gh<=6.8:
            for k in ch: used[k]=True
            ids=set(c['id'] for c in cs)
            out.append(dict(x=x0,y=y0,w=x1-x0,h=y1-y0,gh=gh,n=len(cs),ids=ids,lab=lab))
    return out
def bilinear(img,sx,sy):
    """sample img at float coords; outside = 0 (each of the 4 taps outside contributes 0)"""
    Hh,Ww=img.shape
    x0=np.floor(sx).astype(np.int64); y0=np.floor(sy).astype(np.int64)
    fx=(sx-x0).astype(np.float32); fy=(sy-y0).astype(np.float32)
    def tap(yy,xx):
        ok=(xx>=0)&(xx<Ww)&(yy>=0)&(yy<Hh)
        v=np.zeros(sx.shape,np.float32); v[ok]=img[yy[ok],xx[ok]]; return v
    return (tap(y0,x0)*(1-fx)*(1-fy)+tap(y0,x0+1)*fx*(1-fy)+tap(y0+1,x0)*(1-fx)*fy+tap(y0+1,x0+1)*fx*fy).astype(np.float32)
def resize_linear(img,w,h):
    """bilinear resize with pixel-centre alignment, edge clamped"""
    Hh,Ww=img.shape
    sx=(np.arange(w)+0.5)*Ww/w-0.5; sy=(np.arange(h)+0.5)*Hh/h-0.5
    sx=np.clip(sx,0,Ww-1); sy=np.clip(sy,0,Hh-1)
    X,Y=np.meshgrid(sx,sy)
    x0=np.floor(X).astype(int); y0=np.floor(Y).astype(int)
    x1=np.minimum(x0+1,Ww-1); y1=np.minimum(y0+1,Hh-1)
    fx=X-x0; fy=Y-y0
    return (img[y0,x0]*(1-fx)*(1-fy)+img[y0,x1]*fx*(1-fy)+img[y1,x0]*(1-fx)*fy+img[y1,x1]*fx*fy).astype(np.float32)
def resize_box(img,w,h):
    """area resize: each output pixel averages its footprint with fractional weights"""
    Hh,Ww=img.shape
    def weights(n_in,n_out):
        Wm=np.zeros((n_out,n_in),np.float64); sc=n_in/n_out
        for o in range(n_out):
            a=o*sc; b=(o+1)*sc
            for i in range(int(math.floor(a)),min(n_in,int(math.ceil(b)))):
                ov=min(b,i+1)-max(a,i)
                if ov>0: Wm[o,i]=ov
            Wm[o]/=Wm[o].sum()
        return Wm
    return (weights(Hh,h)@img.astype(np.float64)@weights(Ww,w).T).astype(np.float32)
def rectify(score,c):
    x,y,w,h,gh=c['x'],c['y'],c['w'],c['h'],c['gh']
    Hs,Ws=score.shape
    x0,x1=max(0,int(x-0.6*gh)),min(Ws,int(x+w+0.6*gh)); y0,y1=max(0,int(y-0.8*gh)),min(Hs,int(y+h+0.8*gh))
    if x1-x0<4 or y1-y0<4: return None
    reg=score[y0:y1,x0:x1].copy()
    keep=np.isin(c['lab'][y0:y1,x0:x1],list(c['ids']))
    keep=dilate1(dilate1(keep)); reg[~keep]=0
    ys,xs=np.nonzero(reg>0.3)
    if len(xs)<8: return None
    wt=reg[ys,xs].astype(np.float64); sw=wt.sum()
    mx=(xs*wt).sum()/sw; my=(ys*wt).sum()/sw
    cxx=((xs-mx)**2*wt).sum()/sw; cyy=((ys-my)**2*wt).sum()/sw; cxy=((xs-mx)*(ys-my)*wt).sum()/sw
    ang=0.5*math.atan2(2*cxy,cxx-cyy); ang=max(-0.6,min(0.6,ang))
    ux,uy=math.cos(ang),math.sin(ang); vx,vy=-uy,ux
    s=gh/TH; L=int((x1-x0)/s)+4; Hh=int(1.8*TH)
    PU,PV=np.meshgrid(np.arange(L)-L/2, np.arange(Hh)-Hh/2)
    best=None
    for ki in range(21):
        k=-0.5+0.05*ki
        PU2=PU+k*PV
        p=bilinear(reg,mx+(PU2*ux+PV*vx)*s, my+(PU2*uy+PV*vy)*s)
        sc=float((p.sum(0)**2).sum())
        if best is None or sc>best[0]: best=(sc,p)
    p=best[1]
    rowp=p.sum(1); colp=p.sum(0)
    rows=np.nonzero(rowp>0.25*rowp.max())[0]; cols=np.nonzero(colp>0.12*colp.max())[0]
    if len(rows)<3 or len(cols)<3: return None
    p=p[rows[0]:rows[-1]+1, cols[0]:cols[-1]+1]
    return resize_linear(p,max(4,int(round(p.shape[1]*TH/p.shape[0]))),TH)
def cells(p,n):
    W=p.shape[1]; out=[]
    for i in range(n):
        a,b=int(i*W/n),int((i+1)*W/n)
        c=resize_box(p[:,a:max(b,a+1)],CW,CH).astype(np.float64)
        c=c-c.mean(); nn=np.linalg.norm(c)
        out.append(c/nn if nn>1e-6 else c)
    return out
def read_cells(p,T,mode):
    best=None; W=p.shape[1]
    for n in (2,3,4):
        if W < n*0.45*TH or W > n*2.2*TH: continue
        cs=cells(p,n); s=''; sc=[]
        for i,c in enumerate(cs):
            if i==n-1: allowed='%'
            elif mode=='heat' and n==4 and i==0: allowed='01'
            else: allowed='0123456789'
            ch,v=max(((d,float((c*T[d]).sum())) for d in allowed),key=lambda t:t[1])
            s+=ch; sc.append(v)
        tot=float(np.mean(sc))
        if mode=='shield':
            if n==4 and s!='100%': tot-=0.25
            if n==3 and s[0]=='0': tot-=0.25
        if best is None or tot>best[1]: best=(s,tot)
    return best
def flames(redmask,H):
    lab,comps=label8(redmask)
    return [c for c in comps if 0.006*H<=c['h']<=0.03*H and 0.3<=c['w']/c['h']<=1.5 and 0.15<=c['a']/(c['w']*c['h'])<=0.75]
def read_shield(bgr,H,T):
    """bgr: right-panel ROI. returns best (value_str, score, cand)"""
    sc=cyan(bgr); best=None
    for c in text_candidates(sc,H):
        p=rectify(sc,c)
        if p is None: continue
        r=read_cells(p,T,'shield')
        if r and (best is None or r[1]>best[1]): best=(r[0],r[1],c)
    return best
def read_heat(bgr,H,T):
    sc=orange(bgr); rd=red(bgr); best=None
    for f in flames(rd,H):
        h=f['h']; bx0=int(f['x']+f['w']); by0=int(f['y']-1.3*h); bx1=int(bx0+6.5*h); by1=int(f['y']+1.1*h)
        if by0<0: continue
        sub=sc[by0:by1,bx0:bx1]
        if sub.shape[0]<8 or sub.shape[1]<8: continue
        for c in text_candidates(sub,H):
            if c['x']<1.8*h and 0.6*h<=c['gh']<=1.4*h:
                p=rectify(sub,c)
                if p is None: continue
                r=read_cells(p,T,'heat')
                if r and (best is None or r[1]>best[1]): best=(r[0],r[1],dict(c,x=c['x']+bx0,y=c['y']+by0))
    return best
