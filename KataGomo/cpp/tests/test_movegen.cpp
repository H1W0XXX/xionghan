#include <iostream>
#include <fstream>
#include <vector>
#include <cmath>
#include "../game/board.h"
#include "../game/gamelogic.h"
#include "../external/nlohmann_json/json.hpp"

int main() {
    Board::initHash();
    std::ifstream f("move_gen_test_data.json");
    if (!f.is_open()) {
        std::cerr << "Could not open move_gen_test_data.json" << std::endl;
        return 1;
    }
    nlohmann::json test_cases_data;
    f >> test_cases_data;

    int passed = 0;
    int failed = 0;

    for (const auto& tc : test_cases_data) {
        Board board(13, 13);
        std::vector<int8_t> board_colors = tc["board"].get<std::vector<int8_t>>();
        for (int i = 0; i < 211; i++) {
            board.colors[i] = board_colors[i];
        }
        board.nextPla = (Player)tc["pla"].get<int>();
        board.stage = tc["stage"].get<int>();
        board.midLocs[0] = (Loc)tc["midLoc0"].get<int>();

        int8_t maskOut[211];
        GameLogic::getLegalBitmask(board, board.nextPla, maskOut);

        std::vector<int8_t> expected_mask = tc["mask"].get<std::vector<int8_t>>();
        bool match = true;
        for (int i = 0; i < 211; i++) {
            if ((int)maskOut[i] != (int)expected_mask[i]) {
                match = false;
                break;
            }
        }

        if (match) {
            passed++;
        } else {
            failed++;
            std::cout << "Test failed: case_idx=" << (passed+failed-1) 
                      << " pla=" << (int)board.nextPla 
                      << " stage=" << board.stage 
                      << " midLoc0=" << board.midLocs[0] << std::endl;
            int count = 0;
            for(int i=0; i<211; i++) {
                if((int)maskOut[i] != (int)expected_mask[i]) {
                    Color c = board.colors[i];
                    std::cout << "  Mismatch at " << i << " (r=" << (i/14-1) << " c=" << (i%14-1) << "): got " << (int)maskOut[i] << " exp " << (int)expected_mask[i];
                    if (c == 0) std::cout << " [Empty]";
                    else if (c == 3) std::cout << " [Wall]";
                    else std::cout << " [Piece type=" << (int)getPieceType(c) << " pla=" << (int)getPiecePla(c) << "]";
                    std::cout << std::endl;
                    if(++count > 5) break;
                }
            }
        }
    }

    std::cout << "Tests finished. Total: " << (passed+failed) << ", Passed: " << passed << ", Failed: " << failed << std::endl;
    return failed == 0 ? 0 : 1;
}
