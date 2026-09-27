import ref2, numpy as np, pickle, time
T=pickle.load(open('T_ref2.pkl','rb'))
def rois(W,H):
    return dict(shield=(int(W*0.5),int(H*0.5),int(W*0.97),int(H*0.97)), heat=(int(W*0.25),int(H*0.5),int(W*0.65),int(H*0.95)))
def splash(bgr_roi, c):
    tx=c['x']+c['w']/2; ty=c['y']+c['h']/2; g=c['gh']
    Hh,Ww=bgr_roi.shape[:2]
    x0,x1=max(0,int(tx-11*g)),min(Ww,int(tx+11*g)); y0,y1=max(0,int(ty-11*g)),min(Hh,int(ty+0.5*g))
    if x1<=x0 or y1<=y0: return 0.0,0.0
    b=ref2.blue(bgr_roi[y0:y1,x0:x1])
    n=int(b.sum())
    if n==0: return 0.0,0.0
    xs=np.nonzero(b)[1]+x0
    return n/(g*g), float((xs.mean()-tx)/(6*g))
def read_frame(im):
    H,W=im.shape[:2]; R=rois(W,H); out={}
    x0,y0,x1,y1=R['shield']; r=im[y0:y1,x0:x1]
    s=ref2.read_shield(r,H,T['shield'])
    if s:
        c=s[2]; sp,dx=splash(r,c)
        out['shield']=(s[0],round(s[1],3),c['x']+x0,c['y']+y0,c['w'],c['h'],c['gh'],round(sp,3),round(dx,3))
    x0,y0,x1,y1=R['heat']; r=im[y0:y1,x0:x1]
    h=ref2.read_heat(r,H,T['heat'])
    if h: out['heat']=(h[0],round(h[1],3))
    return out
if __name__=='__main__':
    from common import load
    t=time.time(); print(read_frame(load(121)), round(time.time()-t,1),'s')
