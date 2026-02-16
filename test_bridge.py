import ctypes
import os

# 1. 加载动态库
so_path = os.path.abspath("libxionghan_test.so")
lib = ctypes.CDLL(so_path)

# 2. 定义函数原型
lib.IsLegal.argtypes = [
    ctypes.POINTER(ctypes.c_int8), 
    ctypes.c_int, 
    ctypes.c_int, 
    ctypes.c_int8, 
    ctypes.c_short, 
    ctypes.c_int, 
    ctypes.c_short
]
lib.IsLegal.restype = ctypes.c_bool

# 3. 构造棋盘
stride = 14
board_size = stride * 15
board_data = (ctypes.c_int8 * board_size)(*([3] * board_size))

def set_stone(x, y, color):
    idx = (x + 1) + (y + 1) * stride
    board_data[idx] = color

for y in range(13):
    for x in range(13):
        set_stone(x, y, 0)

# 红方 (P_WHITE=2)
# 帅 (6) -> (2<<4)|6 = 38
set_stone(6, 11, 38)
# 车 (1) -> (2<<4)|1 = 33
set_stone(2, 12, 33)

print("--- Testing stage 0 (Selecting piece) ---")
res = lib.IsLegal(board_data, 13, 13, 2, ctypes.c_short((2+1) + (12+1)*stride), 0, 0)
print(f"Is selection at (2, 12) legal? {res}")

print("\n--- Scanning for ANY legal move ---")
found_any = False
for y in range(13):
    for x in range(13):
        loc = ctypes.c_short((x+1) + (y+1)*stride)
        if lib.IsLegal(board_data, 13, 13, 2, loc, 0, 0):
            print(f"Found legal selection at ({x}, {y})")
            found_any = True

if not found_any:
    print("CRITICAL: No legal moves found!")
else:
    print("SUCCESS: Go bridge is working.")
