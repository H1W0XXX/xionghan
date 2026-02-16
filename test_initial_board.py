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
board_size = 211 # MAX_ARR_SIZE
board_data = (ctypes.c_int8 * board_size)(*([3] * board_size))

def set_stone(x, y, color):
    idx = (x + 1) + (y + 1) * stride
    board_data[idx] = color

for y in range(13):
    for x in range(13):
        set_stone(x, y, 0)

layout = [
    "i.a.h...h.a.i",
    "...bcdedcb...",
    ".............",
    ".f.........f.",
    "..g.g.g.g.g..",
    "j...........j",
    ".............",
    "J...........J",
    "..G.G.G.G.G..",
    ".F.........F.",
    ".............",
    "...BCDEDCB...",
    "I.A.H...H.A.I"
]

PT_ROOK     = 1
PT_KNIGHT   = 2
PT_ELEPHANT = 4
PT_ADVISOR  = 5
PT_KING     = 6
PT_CANNON   = 3
PT_PAWN     = 7
PT_LEI      = 8
PT_FENG     = 9
PT_WEI      = 10

char_to_pt = {
    'a': PT_ROOK,
    'b': PT_KNIGHT,
    'c': PT_ELEPHANT,
    'd': PT_ADVISOR,
    'e': PT_KING,
    'f': PT_CANNON,
    'g': PT_PAWN,
    'h': PT_LEI,
    'i': PT_FENG,
    'j': PT_WEI,
}

P_BLACK = 1
P_WHITE = 2

for y in range(13):
    for x in range(13):
        c = layout[y][x]
        if c == '.':
            continue
        p = P_WHITE if 'A' <= c <= 'Z' else P_BLACK
        pt = char_to_pt[c.lower()]
        set_stone(x, y, (p << 4) | pt)

print("\n--- Scanning for ANY legal move for White (Red) ---")
found_any = False
for y in range(13):
    for x in range(13):
        loc = ctypes.c_short((x+1) + (y+1)*stride)
        if lib.IsLegal(board_data, 13, 13, 2, loc, 0, 0):
            print(f"Found legal selection at ({x}, {y}) for White")
            found_any = True

if not found_any:
    print("CRITICAL: No legal moves found for White at start!")

print("\n--- Scanning for ANY legal move for Black ---")
found_any = False
for y in range(13):
    for x in range(13):
        loc = ctypes.c_short((x+1) + (y+1)*stride)
        if lib.IsLegal(board_data, 13, 13, 1, loc, 0, 0):
            print(f"Found legal selection at ({x}, {y}) for Black")
            found_any = True

if not found_any:
    print("CRITICAL: No legal moves found for Black at start!")
