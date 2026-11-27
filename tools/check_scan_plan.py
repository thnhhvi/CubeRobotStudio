# Usage: python check_scan_plan.py "<JSON list of {id, present, revert}>"
# Kinematic check of the scan servo strings.
import re, json, sys, numpy as np
ANG={'L':-90,'l':-45,'C':0,'r':45,'R':90}
AX={'f':np.array([0,0,1]),'b':np.array([0,0,-1]),'r':np.array([1,0,0]),'l':np.array([-1,0,0])}
PAIR={'f':'b','b':'f','r':'l','l':'r'}
def rot(axis,deg):
    a=np.radians(deg); x,y,z=axis; c,s=np.cos(a),np.sin(a); t=1-c
    return np.array([[t*x*x+c,t*x*y-s*z,t*x*z+s*y],[t*x*y+s*z,t*y*y+c,t*y*z-s*x],[t*x*z-s*y,t*y*z+s*x,t*z*z+c]])
def square(M): return np.allclose(np.abs(np.round(M)),np.abs(M),atol=1e-6)
class Robot:
    def __init__(s):
        s.sl={k:'L' for k in 'FBRL'}; s.sp={k:0 for k in 'fbrl'}; s.M=np.eye(3); s.err=[]
    def run(s,seq,label):
        for grp in re.split(r'\d+',seq):
            cmds=re.findall(r'([FBRLfbrl])([LlCrR])',grp)
            spin={}
            for servo,st in cmds:
                if servo.isupper():
                    if st=='L':
                        if s.sl[servo]!='L':
                            ang=s.sp[servo.lower()]
                            if not square(s.M) or ang%90!=0: s.err.append(f"{label}: clamp {servo} at {ang} deg on a non-square face")
                        s.sl[servo]='L'
                    elif st=='R': s.sl[servo]='R'
                    else: s.err.append(f"{label}: slider {servo} at {st}")
                else:
                    spin[servo]=ANG[st]
            # spinner moves (simultaneous within group)
            moved={k:v-s.sp[k] for k,v in spin.items() if v!=s.sp[k]}
            for k,d in moved.items():
                if s.sl[k.upper()]=='L':
                    p=PAIR[k]
                    if moved.get(p,0)!=-d or s.sl[p.upper()]!='L':
                        s.err.append(f"{label}: {k} turns {d} while clamped without its partner mirroring")
                    others=[o for o in 'fbrl' if o not in (k,p)]
                    if any(s.sl[o.upper()]=='L' for o in others):
                        s.err.append(f"{label}: cube turned by {k} while the other pair is clamped")
                    if any(s.sp[o]%180==0 for o in others):
                        s.err.append(f"{label}: cube turned by {k} while the released pair is vertical ({[s.sp[o] for o in others]})")
            # apply cube rotation once per pair (clockwise from the arm = -angle about its axis)
            done=set()
            for k,d in moved.items():
                if s.sl[k.upper()]=='L' and PAIR[k] not in done:
                    s.M=rot(AX[k],-d)@s.M; done.add(k)
            for k,v in spin.items(): s.sp[k]=v
rob=Robot()
plan=json.loads(sys.argv[1])
z=rot(np.array([0,0,1]),-90); x=rot(np.array([1,0,0]),-90)
expect=np.eye(3)
for st in plan:
    for part in ("present","revert"):
        rob.run(st[part].replace("{d}","200"),f"{st['id']} {part}")
        if part=="present":
            # tilted 45 deg: the face to scan points up-and-towards the robot front
            print(f"{st['id']} lifted: grippers",{k:rob.sp[k] for k in 'fbrl'},"sliders",''.join(rob.sl[k] for k in 'FBRL'))
    if st['id'].startswith('A'): expect=z@expect
    if not np.allclose(rob.M,expect,atol=1e-6): rob.err.append(f"{st['id']}: wrong cube orientation after revert")
print("final grippers",rob.sp,"sliders",rob.sl,"cube home:",np.allclose(rob.M,np.eye(3)))
print("ERRORS:" if rob.err else "no rule violations", *rob.err, sep="\n  ")
