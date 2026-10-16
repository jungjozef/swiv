import sys
from unicorn import *
from unicorn.m68k_const import *
d=open('SWIV.adf','rb').read()
s2=d[0xd5400:0xd5400+0x4000]
uc=Uc(UC_ARCH_M68K,UC_MODE_BIG_ENDIAN)
uc.ctl_set_cpu_model(UC_CPU_M68K_M68000)
uc.mem_map(0,0x200000)
uc.mem_map(0xdff000,0x1000)
uc.mem_write(0x70000,s2)
SENT=0x1000
SP=0x7f000
uc.mem_write(SP,SENT.to_bytes(4,'big'))
uc.reg_write(UC_M68K_REG_A7,SP)
uc.reg_write(UC_M68K_REG_A6,0x400)
# stop when we execute outside the loader or reach sentinel
state={'n':0}
def hook(uc,addr,size,ud):
    state['n']+=1
    if addr==SENT or not (0x70000<=addr<0x74000):
        print("left loader at %06x after %d insns"%(addr,state['n']))
        a=[uc.reg_read(r) for r in (UC_M68K_REG_A0,UC_M68K_REG_A1,UC_M68K_REG_A2,UC_M68K_REG_A3,UC_M68K_REG_A4,UC_M68K_REG_A5,UC_M68K_REG_A6,UC_M68K_REG_A7)]
        print("A:",[hex(x) for x in a])
        uc.emu_stop()
uc.hook_add(UC_HOOK_CODE,hook)
try:
    uc.emu_start(0x70000,0xffffff,count=50_000_000)
except UcError as e:
    print("err",e, hex(uc.reg_read(UC_M68K_REG_PC)))
open('stage2_mem.bin','wb').write(uc.mem_read(0,0x200000))
