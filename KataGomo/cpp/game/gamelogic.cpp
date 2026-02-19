#include "../game/gamelogic.h"

/*
 * gamelogic.cpp
 * Logics of game rules
 * Some other game logics are in board.h/cpp
 *
 * Gomoku as a representive
 */

#include <algorithm>

#include <cassert>

#include <cstring>

#include <iostream>

#include <vector>

#include <utility>

#include <mutex>



using namespace std;



namespace {

  // Helpers to match Go logic

  inline int rowOf(Loc loc, int xSize) { return (loc / (xSize + 1)) - 1; }

  inline int colOf(Loc loc, int xSize) { return (loc % (xSize + 1)) - 1; }

  inline bool onBoard(int r, int c) { return r >= 0 && r < 13 && c >= 0 && c < 13; }

  inline Loc indexOf(int r, int c, int xSize) { return (c + 1) + (r + 1) * (xSize + 1); }



  const int WallRow = 6;

  inline bool pawnPassedWall(Player p, int row) {

    if (p == P_BLACK) return row < WallRow; // Red (Bottom)

    if (p == P_WHITE) return row > WallRow; // Black (Top)

    return false;

  }



  bool kingExists(const Board& board, Player p) {

    for(int i=0; i<Board::MAX_ARR_SIZE; i++) {

      if(board.colors[i] != C_WALL && board.colors[i] != C_EMPTY) {

        if(getPiecePla(board.colors[i]) == p && getPieceType(board.colors[i]) == PT_KING) return true;

      }

    }

    return false;

  }



    int8_t checkWinnerInternal(const Board& board, Player pla) {



      Player oppSide = getOpp(pla);



  



      // 1. 检查自己的王是否还在



      if (!kingExists(board, pla)) return oppSide;



  



      // 2. 检查对手的王是否还在



      if (!kingExists(board, oppSide)) return pla;



  



      // 3. 检查对手是否无棋可走 (仅在完整回合结束，即 stage 0 时检查)



      // 如果在 stage 1 检查，对手肯定没棋走（因为轮到当前玩家继续走第二步），会误判胜负。



      if (board.stage == 0) {



        int8_t mask[Board::MAX_ARR_SIZE];



        GameLogic::getLegalBitmask(board, oppSide, mask);



        bool hasMove = false;



        for(int i=0; i<Board::MAX_ARR_SIZE; i++) {



          if(mask[i]) { hasMove = true; break; }



        }



        if (!hasMove) return pla;



      }



  



      return C_WALL; // 游戏继续



    }



  // Feng (锋) tables

  bool fengStations[211] = {false};

  bool fengRoad[211] = {false};

  std::vector<std::vector<Loc>> fengMoveTable[211];

  bool fengInited = false;

  std::mutex fengMutex;



  void initFengTables(int xSize) {

    if (fengInited) return;

    std::lock_guard<std::mutex> lock(fengMutex);

    if (fengInited) return;



    const int fengStep = 3;

    const Loc startLoc = indexOf(0, 0, xSize);

    

    std::vector<Loc> q;

    fengStations[startLoc] = true;

    q.push_back(startLoc);

    int head = 0;

    int dr[] = {-1, -1, 1, 1};

    int dc[] = {-1, 1, -1, 1};



    while(head < (int)q.size()) {

      Loc now = q[head++];

      int r = rowOf(now, xSize);

      int c = colOf(now, xSize);

      for(int i=0; i<4; i++) {

        int r2 = r + dr[i]*fengStep;

        int c2 = c + dc[i]*fengStep;

        if(onBoard(r2, c2)) {

          Loc to = indexOf(r2, c2, xSize);

          if(!fengStations[to]) {

            fengStations[to] = true;

            q.push_back(to);

          }

        }

      }

    }



    for(int r=0; r<13; r++) {

      for(int c=0; c<13; c++) {

        Loc sq = indexOf(r, c, xSize);

        if(!fengStations[sq]) continue;

        fengRoad[sq] = true;

        for(int i=0; i<4; i++) {

          for(int step=1; step<fengStep; step++) {

            int r2 = r + dr[i]*step;

            int c2 = c + dc[i]*step;

            if(!onBoard(r2, c2)) break;

            fengRoad[indexOf(r2, c2, xSize)] = true;

          }

        }

      }

    }



    for(int r=0; r<13; r++) {

      for(int c=0; c<13; c++) {

        Loc sq = indexOf(r, c, xSize);

        if(!fengRoad[sq]) continue;

        for(int i=0; i<4; i++) {

          std::vector<Loc> line;

          int r2 = r + dr[i], c2 = c + dc[i];

          while(onBoard(r2, c2)) {

            Loc to = indexOf(r2, c2, xSize);

            if(!fengRoad[to]) break;

            line.push_back(to);

            if(fengStations[to]) break;

            r2 += dr[i]; c2 += dc[i];

          }

          if(!line.empty()) fengMoveTable[sq].push_back(line);

        }

      }

    }

    fengInited = true;

  }



  bool kingsFace(const Board& board) {
    int kr[3], kc[3];
    bool found[3] = {false, false, false};
    for(int r=0; r<13; r++) {
      for(int c=0; c<13; c++) {
        Loc loc = indexOf(r, c, board.x_size);
        Color pc = board.colors[loc];
        if(getPieceType(pc) == PT_KING) {
          Player p = getPiecePla(pc);
          kr[p] = r; kc[p] = c;
          found[p] = true;
        }
      }
    }
    if(!found[P_BLACK] || !found[P_WHITE]) return false;
    if(kc[P_BLACK] != kc[P_WHITE]) return false;
    int minRow = std::min(kr[P_BLACK], kr[P_WHITE]);
    int maxRow = std::max(kr[P_BLACK], kr[P_WHITE]);
    for(int r = minRow + 1; r < maxRow; r++) {
      if(board.colors[indexOf(r, kc[P_BLACK], board.x_size)] != C_EMPTY) return false;
    }
    return true;
  }

  bool inPalace(Player p, int r, int c) {
    if(c < 5 || c > 7) return false;
    if(p == P_WHITE) return r >= 1 && r <= 3;
    if(p == P_BLACK) return r >= 9 && r <= 11;
    return false;
  }

  void genMoves(const Board& board, Loc from, std::vector<Loc>& tos) {
    Color pc = board.colors[from];
    Player side = getPiecePla(pc);
    int type = getPieceType(pc);
    int r = rowOf(from, board.x_size);
    int c = colOf(from, board.x_size);

    auto add = [&](int tr, int tc) {
      if(!onBoard(tr, tc)) return false;
      Loc to = indexOf(tr, tc, board.x_size);
      Color dest = board.colors[to];
      if(dest == C_EMPTY || getPiecePla(dest) != side) {
        tos.push_back(to);
        return true;
      }
      return false;
    };

    switch(type) {
      case PT_ROOK: {
        int dr[] = {-1, 1, 0, 0}, dc[] = {0, 0, -1, 1};
        for(int i=0; i<4; i++) {
          for(int step=1;;step++) {
            int tr = r + dr[i]*step, tc = c + dc[i]*step;
            if(!onBoard(tr, tc)) break;
            Loc to = indexOf(tr, tc, board.x_size);
            if(board.colors[to] == C_EMPTY) tos.push_back(to);
            else {
              if(getPiecePla(board.colors[to]) != side) tos.push_back(to);
              break;
            }
          }
        }
        break;
      }
      case PT_CANNON: {
        int dr[] = {-1, 1, 0, 0}, dc[] = {0, 0, -1, 1};
        for(int i=0; i<4; i++) {
          bool jumped = false;
          for(int step=1;;step++) {
            int tr = r + dr[i]*step, tc = c + dc[i]*step;
            if(!onBoard(tr, tc)) break;
            Loc to = indexOf(tr, tc, board.x_size);
            if(!jumped) {
              if(board.colors[to] == C_EMPTY) tos.push_back(to);
              else jumped = true;
            } else {
              if(board.colors[to] != C_EMPTY) {
                if(getPiecePla(board.colors[to]) != side) tos.push_back(to);
                break;
              }
            }
          }
        }
        break;
      }
      case PT_KNIGHT: {
        int dr[] = {-2, -2, -1, -1, 1, 1, 2, 2}, dc[] = {-1, 1, -2, 2, -2, 2, -1, 1};
        int br[] = {-1, -1, 0, 0, 0, 0, 1, 1}, bc[] = {0, 0, -1, 1, -1, 1, 0, 0};
        for(int i=0; i<8; i++) {
          if(onBoard(r+br[i], c+bc[i]) && board.colors[indexOf(r+br[i], c+bc[i], board.x_size)] == C_EMPTY)
            add(r+dr[i], c+dc[i]);
        }
        int sdr[] = {-1, 1, 0, 0}, sdc[] = {0, 0, -1, 1};
        for(int i=0; i<4; i++) {
          if(onBoard(r+sdr[i], c+sdc[i]) && board.colors[indexOf(r+sdr[i], c+sdc[i], board.x_size)] == C_EMPTY &&
             onBoard(r+2*sdr[i], c+2*sdc[i]) && board.colors[indexOf(r+2*sdr[i], c+2*sdc[i], board.x_size)] == C_EMPTY)
            add(r+3*sdr[i], c+3*sdc[i]);
        }
        break;
      }
      case PT_ELEPHANT: {
        int dr[] = {-2, -2, 2, 2}, dc[] = {-2, 2, -2, 2};
        for(int i=0; i<4; i++) {
          int mr = r + dr[i]/2, mc = c + dc[i]/2;
          int tr = r + dr[i], tc = c + dc[i];
          if(onBoard(tr, tc) && board.colors[indexOf(mr, mc, board.x_size)] == C_EMPTY) {
            if((side == P_WHITE && tr <= WallRow) || (side == P_BLACK && tr >= WallRow))
              add(tr, tc);
          }
        }
        break;
      }
      case PT_ADVISOR: {
        int dr[] = {-1, -1, 1, 1}, dc[] = {-1, 1, -1, 1};
        for(int i=0; i<4; i++) {
          if(inPalace(side, r+dr[i], c+dc[i])) add(r+dr[i], c+dc[i]);
        }
        break;
      }
      case PT_KING: {
        int dr[] = {-1, 1, 0, 0}, dc[] = {0, 0, -1, 1};
        for(int i=0; i<4; i++) {
          if(inPalace(side, r+dr[i], c+dc[i])) add(r+dr[i], c+dc[i]);
        }
        break;
      }
      case PT_PAWN: {
        int dir = (side == P_BLACK ? -1 : 1);
        if(pawnPassedWall(side, r)) {
          add(r + dir, c); add(r, c - 1); add(r, c + 1);
        } else {
          for(int step=1;;step++) {
            int tr = r + dir*step;
            if(!onBoard(tr, c)) break;
            Loc to = indexOf(tr, c, board.x_size);
            if(board.colors[to] == C_EMPTY) {
              tos.push_back(to);
              if(pawnPassedWall(side, tr)) break;
            } else {
              if(step == 1 && getPiecePla(board.colors[to]) != side) tos.push_back(to);
              break;
            }
          }
        }
        break;
      }
      case PT_LEI: {
        // Use ring order clockwise: Right, Down-Right, Down, Down-Left, Left, Up-Left, Up, Up-Right
        int rdr[] = {0, 1, 1, 1, 0, -1, -1, -1};
        int rdc[] = {1, 1, 0, -1, -1, -1, 0, 1};
        
        // 1. Move logic (queen-like, only empty)
        for(int i=0; i<8; i++) {
          for(int step=1;;step++) {
            int tr = r + rdr[i]*step, tc = c + rdc[i]*step;
            if(!onBoard(tr, tc)) break;
            Loc to = indexOf(tr, tc, board.x_size);
            if(board.colors[to] == C_EMPTY) tos.push_back(to);
            else break;
          }
        }
        
        // 2. Capture logic (adjacent ring, lone piece)
        for(int i=0; i<8; i++) {
          int tr = r + rdr[i], tc = c + rdc[i];
          if(!onBoard(tr, tc)) continue;
          Loc to = indexOf(tr, tc, board.x_size);
          Color target = board.colors[to];
          if(target != C_EMPTY && getPiecePla(target) != side) {
            bool lone = true;
            int prev = (i + 7) % 8;
            int next = (i + 1) % 8;
            for(int idx : {prev, next}) {
              int lr = r + rdr[idx], lc = c + rdc[idx];
              if(onBoard(lr, lc)) {
                if(board.colors[indexOf(lr, lc, board.x_size)] != C_EMPTY) {
                  lone = false;
                  break;
                }
              }
            }
            if(lone) tos.push_back(to);
          }
        }
        break;
      }
      case PT_FENG: {
        initFengTables(board.x_size);
        if(!fengRoad[from]) break;
        bool canAttack = fengStations[from];
        for(const auto& line : fengMoveTable[from]) {
          for(Loc to : line) {
            if(board.colors[to] == C_EMPTY) tos.push_back(to);
            else {
              if(canAttack && getPiecePla(board.colors[to]) != side) tos.push_back(to);
              break;
            }
            if(fengStations[to]) break;
          }
        }
        break;
      }
      case PT_WEI: {
        for(int dc_dir : {-1, 1}) {
          for(int step=2;;step+=2) {
            int tc = c + dc_dir*step;
            if(!onBoard(r, tc)) break;
            bool blocked = false;
            for(int mc = c + dc_dir; mc != tc; mc += dc_dir) {
              if(board.colors[indexOf(r, mc, board.x_size)] != C_EMPTY) { blocked = true; break; }
            }
            if(blocked) break;
            if(board.colors[indexOf(r, tc, board.x_size)] == C_EMPTY) tos.push_back(indexOf(r, tc, board.x_size));
            else break;
          }
          int mc = c + dc_dir, tc = c + 2*dc_dir;
          if(onBoard(r, mc) && onBoard(r, tc) && board.colors[indexOf(r, mc, board.x_size)] == C_EMPTY) {
            Color target = board.colors[indexOf(r, tc, board.x_size)];
            if(target != C_EMPTY && getPiecePla(target) != side) tos.push_back(indexOf(r, tc, board.x_size));
          }
        }
        break;
      }
    }
  }
}

bool GameLogic::isLegal(const Board& board, Player pla, Loc loc) {
  if(loc <= 1) return false;
  int8_t mask[Board::MAX_ARR_SIZE];
  getLegalBitmask(board, pla, mask);
  return mask[loc] != 0;
}

bool GameLogic::isAttacked(const Board& board, Loc sq, Player bySide) {
  for(int r=0; r<13; r++) {
    for(int c=0; c<13; c++) {
      Loc from = indexOf(r, c, board.x_size);
      if(board.colors[from] != C_EMPTY && board.colors[from] != C_WALL && getPiecePla(board.colors[from]) == bySide) {
        std::vector<Loc> tos;
        genMoves(board, from, tos);
        for(Loc to : tos) {
          if(to == sq) return true;
        }
      }
    }
  }
  return false;
}

bool GameLogic::isInCheck(const Board& board, Player side) {
  for(int i=0; i<Board::MAX_ARR_SIZE; i++) {
    if(board.colors[i] != C_EMPTY && board.colors[i] != C_WALL) {
      if(getPiecePla(board.colors[i]) == side && getPieceType(board.colors[i]) == PT_KING) {
        return isAttacked(board, i, getOpp(side));
      }
    }
  }
  return false;
}

void GameLogic::getLegalBitmask(const Board& board, Player pla, int8_t* maskOut) {
  for(int i=0; i<Board::MAX_ARR_SIZE; i++) maskOut[i] = 0;
  
  std::vector<std::pair<Loc, Loc>> legalMoves;
  for(int r=0; r<13; r++) {
    for(int c=0; c<13; c++) {
      Loc from = indexOf(r, c, board.x_size);
      if(board.colors[from] != C_EMPTY && getPiecePla(board.colors[from]) == pla) {
        std::vector<Loc> tos;
        genMoves(board, from, tos);
        for(Loc to : tos) {
          Board nextBoard = board;
          nextBoard.colors[to] = nextBoard.colors[from];
          nextBoard.colors[from] = C_EMPTY;
          if(!kingsFace(nextBoard)) {
            legalMoves.push_back({from, to});
          }
        }
      }
    }
  }

  if(board.stage == 0) {
    for(auto& mv : legalMoves) maskOut[mv.first] = 1;
  } else {
    for(auto& mv : legalMoves) {
      if(mv.first == board.midLocs[0]) maskOut[mv.second] = 1;
    }
  }
}

Loc GameLogic::findImmediateKingCapture(const Board& board, Player pla) {
  std::vector<std::pair<Loc, Loc>> legalMoves;
  for(int r=0; r<13; r++) {
    for(int c=0; c<13; c++) {
      Loc from = indexOf(r, c, board.x_size);
      if(board.colors[from] != C_EMPTY && getPiecePla(board.colors[from]) == pla) {
        std::vector<Loc> tos;
        genMoves(board, from, tos);
        for(Loc to : tos) {
          Board nextBoard = board;
          nextBoard.colors[to] = nextBoard.colors[from];
          nextBoard.colors[from] = C_EMPTY;
          if(!kingsFace(nextBoard)) {
            legalMoves.push_back({from, to});
          }
        }
      }
    }
  }

  auto isKing = [&](Loc to) {
    Color destColor = board.colors[to];
    if (destColor == C_EMPTY || destColor == C_WALL || getPiecePla(destColor) == pla) return false;
    return getPieceType(destColor) == PT_KING;
  };

  if(board.stage == 1) {
    Loc from = board.midLocs[0];
    for(auto& mv : legalMoves) {
      if(mv.first == from) {
        if(isKing(mv.second)) return mv.second;
      }
    }
  } else {
    for(auto& mv : legalMoves) {
      if(isKing(mv.second)) return mv.first;
    }
  }
  return Board::NULL_LOC;
}

float GameLogic::getMoveValueGain(const Board& board, Player pla, Loc loc) {
  if (board.stage == 0) return 0.0f; 
  
  Color destColor = board.colors[loc];
  if (destColor == C_EMPTY || destColor == C_WALL || getPiecePla(destColor) == pla) {
    return 0.0f;
  }
  
  int type = getPieceType(destColor);
  switch(type) {
    case PT_ROOK:     return 5.0f;
    case PT_CANNON:   return 4.8f;
    case PT_KNIGHT:   return 4.6f;
    case PT_ELEPHANT: return 2.5f;
    case PT_ADVISOR:  return 2.5f;
    case PT_KING:     return 100.0f;
    case PT_PAWN:     return 1.2f;
    case PT_LEI:      return 5.2f;
    case PT_FENG:     return 3.6f;
    case PT_WEI:      return 2.6f;
  }
  return 0.0f;
}

GameLogic::MovePriority GameLogic::getMovePriorityAssumeLegal(const Board& board, const BoardHistory& hist, Player pla, Loc loc) {
  return MP_NORMAL;
}

GameLogic::MovePriority GameLogic::getMovePriority(const Board& board, const BoardHistory& hist, Player pla, Loc loc) {
  if(!board.isLegal(loc, pla))
    return MP_ILLEGAL;
  return getMovePriorityAssumeLegal(board, hist, pla, loc);
}

float GameLogic::getApproxScore(const Board& board) {
  float score = 0.0f;
  for(int i=0; i<Board::MAX_ARR_SIZE; i++) {
    Color pc = board.colors[i];
    if(pc == C_EMPTY || pc == C_WALL) continue;
    
    Player p = getPiecePla(pc);
    int type = getPieceType(pc);
    float val = 0;
    switch(type) {
      case PT_ROOK:     val = 5.0f; break;
      case PT_CANNON:   val = 4.8f; break;
      case PT_KNIGHT:   val = 4.6f; break;
      case PT_ELEPHANT: val = 2.5f; break;
      case PT_ADVISOR:  val = 2.5f; break;
      case PT_KING:     val = 100.0f; break;
      case PT_PAWN:     val = 1.2f; break;
      case PT_LEI:      val = 5.2f; break;
      case PT_FENG:     val = 3.6f; break;
      case PT_WEI:      val = 2.6f; break;
    }
    if(p == P_BLACK) score += val; // Red in Go
    else score -= val;             // Black in Go
  }
  return score;
}

Color GameLogic::checkWinnerAfterPlayed(
  const Board& board,
  const BoardHistory& hist,
  Player pla,
  Loc loc) {
  return (Color)checkWinnerInternal(board, pla);
}

GameLogic::ResultsBeforeNN::ResultsBeforeNN() {
  inited = false;
  winner = C_WALL;
  myOnlyLoc = Board::NULL_LOC;
}

void GameLogic::ResultsBeforeNN::init(const Board& board, const BoardHistory& hist, Color nextPlayer) {
  if(inited)
    return;
  inited = true;

  int8_t legalBitmask[Board::MAX_ARR_SIZE];
  getLegalBitmask(board, nextPlayer, legalBitmask);

  int legalCount = 0;
  for(int x = 0; x < board.x_size; x++) {
    for(int y = 0; y < board.y_size; y++) {
      Loc loc = Location::getLoc(x, y, board.x_size);
      if (legalBitmask[loc]) {
        legalCount++;
        MovePriority mp = getMovePriority(board, hist, nextPlayer, loc);
        if(mp == MP_SUDDEN_WIN || mp == MP_WINNING) {
          winner = nextPlayer;
          myOnlyLoc = loc;
          return;
        }
      }
    }
  }

  if (legalCount == 0 && board.stage == 0) {
    winner = getOpp(nextPlayer);
  }

  return;
}
