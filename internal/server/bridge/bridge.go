package main

/*
#include <stdbool.h>
#include <stdint.h>
*/
import "C"
import (
	"fmt"
	"time"
	"unsafe"
	"xionghan/internal/xionghan"
)

//export IsLegal
func IsLegal(boardPtr *C.int8_t, xSize, ySize C.int, pla C.int8_t, loc C.short, stage C.int, midLoc0 C.short) C.bool {
	if loc <= 1 { return C.bool(false) }
	b := cToGoBoard(boardPtr, xSize, ySize)
	side := cToGoSide(pla)
	pos := &xionghan.Position{Board: b, SideToMove: side}
	gSq := cToGoSq(loc, xSize)
	if gSq < 0 || gSq >= 169 { return C.bool(false) }
	legalMoves := pos.GenerateLegalMoves(false)
	if stage == 0 {
		for _, lm := range legalMoves {
			if lm.From == gSq { return C.bool(true) }
		}
	} else {
		from := cToGoSq(midLoc0, xSize)
		for _, lm := range legalMoves {
			if lm.From == from && lm.To == gSq { return C.bool(true) }
		}
	}
	return C.bool(false)
}

//export GetLegalBitmask
func GetLegalBitmask(boardPtr *C.int8_t, xSize, ySize C.int, pla C.int8_t, stage C.int, midLoc0 C.short, maskOut *C.int8_t) {
	start := time.Now()
	b := cToGoBoard(boardPtr, xSize, ySize)
	side := cToGoSide(pla)
	pos := &xionghan.Position{Board: b, SideToMove: side}
	
	// 生成合法走法
	legalMoves := pos.GenerateLegalMoves(false)

	mask := (*[211]int8)(unsafe.Pointer(maskOut))
	for i := 0; i < 211; i++ { mask[i] = 0 }

	count := 0
	if stage == 0 {
		for _, lm := range legalMoves {
			cLoc := (lm.From % 13 + 1) + (lm.From / 13 + 1) * (int(xSize) + 1)
			if cLoc >= 0 && cLoc < 211 { 
				if mask[cLoc] == 0 {
					mask[cLoc] = 1 
					count++
				}
			}
		}
	} else {
		from := cToGoSq(midLoc0, xSize)
		for _, lm := range legalMoves {
			if lm.From == from {
				cLoc := (lm.To % 13 + 1) + (lm.To / 13 + 1) * (int(xSize) + 1)
				if cLoc >= 0 && cLoc < 211 { 
					if mask[cLoc] == 0 {
						mask[cLoc] = 1 
						count++
					}
				}
			}
		}
	}
	
	// 每 1000 次打印一次详情，或者如果处理太慢就打印
	elapsed := time.Since(start)
	if elapsed > 100 * time.Millisecond {
		fmt.Printf("[Go Bridge] SLOW CALL: side=%v, stage=%d, legalCount=%d, took=%v\n", side, stage, count, elapsed)
	}
}

//export CheckWinner
func CheckWinner(boardPtr *C.int8_t, xSize, ySize C.int, pla C.int8_t) C.int8_t {
	b := cToGoBoard(boardPtr, xSize, ySize)
	// pla 是刚走完的人（当前局面的胜者候选）。
	side := cToGoSide(pla)
	oppSide := xionghan.Side(1 - int(side))

	// 1. 检查自己的王是否还在（防止自杀或异常）
	posSelf := &xionghan.Position{Board: b, SideToMove: side}
	if !posSelf.KingExists(side) {
		return C.int8_t(3 - int(pla)) // 自己王没了，对方赢
	}

	// 2. 检查对手的王是否还在
	posOpp := &xionghan.Position{Board: b, SideToMove: oppSide}
	if !posOpp.KingExists(oppSide) {
		return pla // 对手王被吃了，pla 赢
	}

	// 3. 检查对手是否被将死或困毙（无合法招法）
			if len(posOpp.GenerateLegalMoves(false)) == 0 {		return pla // 对手没棋走了，pla 赢
	}

	return 3 // C_WALL (游戏继续)
}

func cToGoBoard(ptr *C.int8_t, xSize, ySize C.int) xionghan.Board {
	var b xionghan.Board
	stride := int(xSize) + 1
	slice := (*[211]C.int8_t)(unsafe.Pointer(ptr))
	for y := 0; y < 13; y++ {
		for x := 0; x < 13; x++ {
			cLoc := (x + 1) + (y + 1)*stride
			val := slice[cLoc]
			if val == 0 || val == 3 { continue }
			cPla := val >> 4
			cType := val & 0xF
			if cPla == 1 { b.Squares[y*13+x] = xionghan.Piece(cType) } else { b.Squares[y*13+x] = xionghan.Piece(-int8(cType)) }
		}
	}
	return b
}

func cToGoSide(pla C.int8_t) xionghan.Side {
	if pla == 1 { return xionghan.Red }
	return xionghan.Black
}

func cToGoSq(loc C.short, xSize C.int) int {
	stride := int(xSize) + 1
	x := int(loc%C.short(stride)) - 1
	y := int(loc/C.short(stride)) - 1
	if x < 0 || x >= 13 || y < 0 || y >= 13 { return -1 }
	return y*13 + x
}

func main() {}
