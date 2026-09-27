import ref3, numpy as np, pickle
T=pickle.load(open('T_ref3.pkl','rb'))
def read_frame(im,pal=ref3.DEFAULT_PALETTE):
    H,W=im.shape[:2]; out={}
    x0,y0,x1,y1=int(W*0.5),int(H*0.5),int(W*0.97),int(H*0.97); r=im[y0:y1,x0:x1]
    s=ref3.read_shield(r,H,T['shield'],pal)
    if s:
        c=s[2]; sp,dx=ref3.splash(r,c,pal)
        out['shield']=(s[0],round(s[1],3),c['x']+x0,c['y']+y0,c['w'],c['h'],c['gh'],round(sp,3),round(dx,3))
    x0,y0,x1,y1=int(W*0.25),int(H*0.5),int(W*0.65),int(H*0.95); r=im[y0:y1,x0:x1]
    h=ref3.read_heat(r,H,T['heat'],pal)
    if h: out['heat']=(h[0],round(h[1],3))
    return out
