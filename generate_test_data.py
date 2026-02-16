import ctypes
import os
import json
import random

# Load the shared library
so_path = os.path.abspath("libxionghan_test.so")
lib = ctypes.CDLL(so_path)

# Define function prototypes
lib.GetLegalBitmask.argtypes = [
    ctypes.POINTER(ctypes.c_int8), 
    ctypes.c_int, 
    ctypes.c_int, 
    ctypes.c_int8, 
    ctypes.c_int, 
    ctypes.c_short,
    ctypes.POINTER(ctypes.c_int8)
]
lib.GetLegalBitmask.restype = None

STRIDE = 14
MAX_ARR_SIZE = 211

def get_loc(x, y):
    return (x + 1) + (y + 1) * STRIDE

def generate_random_board():
    board = [3] * MAX_ARR_SIZE
    for y in range(13):
        for x in range(13):
            board[get_loc(x, y)] = 0
    
    pieces = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10] # PT_*
    players = [1, 2] # P_BLACK(1), P_WHITE(2)
    
    # Always place kings to avoid crash or instant end if logic requires it
    # P_BLACK king
    board[get_loc(6, 0)] = (1 << 4) | 6
    # P_WHITE king
    board[get_loc(6, 12)] = (2 << 4) | 6
    
    num_extra_pieces = random.randint(5, 20)
    for _ in range(num_extra_pieces):
        x, y = random.randint(0, 12), random.randint(0, 12)
        loc = get_loc(x, y)
        if board[loc] == 0:
            p = random.choice(players)
            pt = random.choice(pieces)
            board[loc] = (p << 4) | pt
            
    return board

def get_mask(board, pla, stage, midLoc0):
    board_data = (ctypes.c_int8 * MAX_ARR_SIZE)(*board)
    mask_out = (ctypes.c_int8 * MAX_ARR_SIZE)()
    lib.GetLegalBitmask(board_data, 13, 13, pla, stage, midLoc0, mask_out)
    return list(mask_out)

test_cases = []
print("Generating test cases...")

# 1. Initial board
initial_board = [3] * MAX_ARR_SIZE
for y in range(13):
    for x in range(13):
        initial_board[get_loc(x, y)] = 0
# Simplified initial layout for testing if needed, or just use random.
# Let's use 50 random boards.
for i in range(50):
    board = generate_random_board()
    for pla in [1, 2]:
        # Stage 0
        mask0 = get_mask(board, pla, 0, 0)
        test_cases.append({
            "board": board,
            "pla": pla,
            "stage": 0,
            "midLoc0": 0,
            "mask": mask0
        })
        
        # Stage 1: pick a random legal piece from stage 0
        legal_froms = [loc for loc, val in enumerate(mask0) if val == 1]
        if legal_froms:
            midLoc0 = random.choice(legal_froms)
            mask1 = get_mask(board, pla, 1, midLoc0)
            test_cases.append({
                "board": board,
                "pla": pla,
                "stage": 1,
                "midLoc0": midLoc0,
                "mask": mask1
            })

with open("move_gen_test_data.json", "w") as f:
    json.dump(test_cases, f)

print(f"Generated {len(test_cases)} test cases in move_gen_test_data.json")
