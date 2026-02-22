// ====== 棋盘坐标相关：和 SVG 网格对齐 ======
const ROWS = 13;
const COLS = 13;
const GRID_START = 50;   // 第一条线的位置（50,50）
const GRID_STEP  = 100;  // 格子间距
const PIECE_MOVE_SPEED = 900; // 棋子平移速度，棋盘坐标单位/秒

function sqToRC(sq) {
    return { r: Math.floor(sq / COLS), c: sq % COLS };
}

function rcToSq(r, c) {
    return r * COLS + c;
}

function sqCenter(sq) {
    const { r, c } = sqToRC(sq);
    const x = GRID_START + GRID_STEP * c;
    const y = GRID_START + GRID_STEP * r;
    return { x, y };
}

// 把鼠标点击转换为棋盘格号
function svgPointToSquare(evt) {
    const svg = document.getElementById("boardSvg");
    const pt = svg.createSVGPoint();
    pt.x = evt.clientX;
    pt.y = evt.clientY;
    const svgP = pt.matrixTransform(svg.getScreenCTM().inverse());

    const col = Math.round((svgP.x - GRID_START) / GRID_STEP);
    const row = Math.round((svgP.y - GRID_START) / GRID_STEP);

    if (row < 0 || row >= ROWS || col < 0 || col >= COLS) return null;
    return rcToSq(row, col);
}

// ====== 全局状态 ======
let gameId = null;
let board = new Array(ROWS * COLS).fill(null); // 每格 { ch, side } 或 null
let sideToMove = 0;  // 0=红(w), 1=黑(b)
let legalMoves = []; // 后端返回的伪合法招 [{from,to},...]
let selectedSq = null;
let movesFromSelected = []; // 当前选中棋子对应的所有可走棋（子集）
let currentFen = ""; // ✅ 当前局面的 FEN 字符串（直接用后端给的）
let lastMove = null; // 上一步走子 {from, to}
let moveCount = 0; // 当前对局累计步数（人类+AI）


// ====== Cookie 工具（session cookie：不设 expires 就会在关闭浏览器时清空） ======
function setSessionCookie(name, value) {
    document.cookie = `${encodeURIComponent(name)}=${encodeURIComponent(value)}; path=/`;
}

function getCookie(name) {
    const target = encodeURIComponent(name) + "=";
    const parts = document.cookie.split(";");
    for (let part of parts) {
        part = part.trim();
        if (part.startsWith(target)) {
            return decodeURIComponent(part.substring(target.length));
        }
    }
    return null;
}

function deleteCookie(name) {
    document.cookie = `${encodeURIComponent(name)}=; path=/; max-age=0`;
}

function updateMoveCountUI() {
    const moveCountEl = document.getElementById("moveCount");
    if (moveCountEl) moveCountEl.innerText = String(moveCount);
}

function loadMoveCountFromSession() {
    const cookieGameId = getCookie("xionghan_move_count_game_id");
    const cookieMoveCount = parseInt(getCookie("xionghan_move_count") || "", 10);
    if (cookieGameId === gameId && Number.isFinite(cookieMoveCount) && cookieMoveCount >= 0) {
        moveCount = cookieMoveCount;
    } else {
        moveCount = 0;
    }
    updateMoveCountUI();
}

function saveMoveCountToSession() {
    if (!gameId) return;
    setSessionCookie("xionghan_move_count_game_id", gameId);
    setSessionCookie("xionghan_move_count", String(moveCount));
}

function resetMoveCount() {
    moveCount = 0;
    updateMoveCountUI();
    saveMoveCountToSession();
}


// ====== FEN 解析（对应后端 fen.go 的 Encode） ======
function updateBoardFromFen(fen) {
    currentFen = fen.trim();
    const [boardStr, stm] = fen.trim().split(" ");
    sideToMove = stm === "w" ? 0 : 1;
    const rows = boardStr.split("/");
    board = new Array(ROWS * COLS).fill(null);

    const ZERO = "0".charCodeAt(0);

    for (let r = 0; r < ROWS; r++) {
        const rowStr = rows[r] || "";
        let c = 0;
        for (const ch of rowStr) {
            if (c >= COLS) break;

            const code = ch.charCodeAt(0);

            if ((ch >= "1" && ch <= "9") || (ch >= ":" && ch <= "=")) {
                const n = code - ZERO; // '1'→1, '3'→3, ':'→10, ';'→11, '<'→12, '='→13
                c += n;
                continue;
            }
            if (ch === ".") {
                c++;
                continue;
            }

            // 其余才是棋子
            const isUpper = ch === ch.toUpperCase();
            const base = ch.toLowerCase();
            const side = isUpper ? 0 : 1; // 0=红,1=绿(黑)
            board[r * COLS + c] = { ch: base, side };
            c++;
        }
    }
}


// ====== 映射棋子 -> SVG 文件名 ======
function pieceSprite(info) {
    if (!info) return null;
    const sidePrefix = info.side === 0 ? "r" : "b"; // 0=红, 1=绿(黑)
    const base = info.ch; // a..j

    let kind = null;
    switch (base) {
        case "a": kind = "chariot";  break; // 车
        case "b": kind = "horse";    break; // 马
        case "c": kind = "elephant";   break; // 相 / 都
        case "d": kind = "advisor"; break; // 士 / 氏
        case "e": kind = "king";  break; // 皇 / 单
        case "f": kind = "cannon";     break; // 炮
        case "g": kind = "pawn";     break; // 兵 / 卒
        case "h": kind = "catapult";    break; // 檑（礌）
        case "i": kind = "train"; break; // 锋 / 锐
        case "j": kind = "guard";    break; // 卫（尉）

        default:
            return null;
    }
    return `svg/pieces/${sidePrefix}-${kind}.svg`;
}



// ====== 渲染 ======
function isGameOver() {
    let redKing = false;
    let blackKing = false;
    for (let i = 0; i < board.length; i++) {
        if (board[i] && board[i].ch === 'e') {
            if (board[i].side === 0) redKing = true;
            else if (board[i].side === 1) blackKing = true;
        }
    }
    
    const statusEl = document.getElementById("gameStatus");
    if (!redKing) {
        if (statusEl) statusEl.innerText = "黑方胜 (Black Wins)";
        return true;
    }
    if (!blackKing) {
        if (statusEl) statusEl.innerText = "红方胜 (Red Wins)";
        return true;
    }
    if (statusEl) statusEl.innerText = "进行中 (Playing)";
    return false;
}

function renderBoard(animMove) {
    const piecesLayer = document.getElementById("piecesLayer");
    const highlightLayer = document.getElementById("highlightLayer");
    highlightLayer.innerHTML = "";

    const gameOver = isGameOver(); // 更新状态显示
    const btnAi = document.getElementById("btnAiMove");
    if (btnAi) btnAi.disabled = gameOver;

    // ============ 高亮渲染（不变） ============
    if (lastMove) {
        const from = sqCenter(lastMove.from);
        const to = sqCenter(lastMove.to);
        // 小圈在起始点
        addCircle(highlightLayer, from.x, from.y, 25, "blue", 3, 0.3);
        // 大圈在目标点
        addCircle(highlightLayer, to.x, to.y, 45, "blue", 3, 0.5);
    }

    if (selectedSq !== null) {
        const { x, y } = sqCenter(selectedSq);
        addCircle(highlightLayer, x, y, 36, "#ffcc00", 6);
    }

    for (const mv of movesFromSelected) {
        const { x, y } = sqCenter(mv.to);
        const isCapture = board[mv.to] !== null;

        if (isCapture) addCircle(highlightLayer, x, y, 36, "#ff3333", 6, 0.9);
        else addDot(highlightLayer, x, y, 18, "blue", 0.45);
    }

    // ============ 棋子 DOM复用 ============

    const existing = new Map();
    piecesLayer.querySelectorAll("image").forEach(img => {
        existing.set(parseInt(img.dataset.sq), img);
    });

    for (let sq = 0; sq < ROWS * COLS; sq++) {
        const info = board[sq];
        const old = existing.get(sq);

        if (!info) {
            // 原来有棋子，现在没了 → 清除
            if (old) old.remove();
            continue;
        }

        const href = pieceSprite(info);
        const { x, y } = sqCenter(sq);
        const size = 80;

        if (old) {
            // 已存在 → 平滑移动棋子，不闪
            old.setAttribute("x", x - size/2);
            old.setAttribute("y", y - size/2);
            old.setAttributeNS("http://www.w3.org/1999/xlink", "href", href);
            continue;
        }

        // 新棋子 → append（仅新增），不会整层重绘
        const img = document.createElementNS("http://www.w3.org/2000/svg", "image");
        img.dataset.sq = sq;
        img.setAttributeNS("http://www.w3.org/1999/xlink","href",href);
        img.setAttribute("x",x-size/2);
        img.setAttribute("y",y-size/2);
        img.setAttribute("width",size);
        img.setAttribute("height",size);
        piecesLayer.appendChild(img);
    }

    if (animMove) animatePieceMove(animMove);
}

// 小工具函数，方便阅读
function addCircle(layer,x,y,r,color,width,opacity=1){
    const el=document.createElementNS("http://www.w3.org/2000/svg","circle");
    el.setAttribute("cx",x); el.setAttribute("cy",y);
    el.setAttribute("r",r);
    el.setAttribute("fill","none");
    el.setAttribute("stroke",color);
    el.setAttribute("stroke-width",width);
    el.setAttribute("opacity",opacity);
    layer.appendChild(el);
}


function expandFen(fen) {
    // 仅转换棋盘部分，不动行棋方
    const parts = fen.split(" ");
    const rows = parts[0].split("/");
    const stm = parts[1];

    const expandedRows = rows.map(row => {
        let out = "";
        for (const ch of row) {
            if (ch >= '1' && ch <= '9') {
                out += ".".repeat(ch.charCodeAt(0) - "0".charCodeAt(0));
            } else if (ch >= ':' && ch <= '=') {
                // ':'=58 => 58-48 = 10
                out += ".".repeat(ch.charCodeAt(0) - "0".charCodeAt(0));
            } else if (ch === '.') {
                out += ".";
            } else {
                // 棋子
                out += ch;
            }
        }
        return out;
    });

    return expandedRows.join("/") + " " + stm;
}


function addDot(layer,x,y,r,color,opacity){
    const el=document.createElementNS("http://www.w3.org/2000/svg","circle");
    el.setAttribute("cx",x); el.setAttribute("cy",y);
    el.setAttribute("r",r);
    el.setAttribute("fill",color);
    el.setAttribute("opacity",opacity);
    layer.appendChild(el);
}

function animatePieceMove(mv){
    if (!mv || mv.from === undefined || mv.to === undefined) return;

    const piecesLayer = document.getElementById("piecesLayer");
    const moving = piecesLayer.querySelector(`image[data-sq="${mv.to}"]`);
    if (!moving) return;

    const from = sqCenter(mv.from);
    const size = parseFloat(moving.getAttribute("width")) || 0;
    const startX = from.x - size / 2;
    const startY = from.y - size / 2;
    const targetX = parseFloat(moving.getAttribute("x"));
    const targetY = parseFloat(moving.getAttribute("y"));

    const dx = targetX - startX;
    const dy = targetY - startY;
    const distance = Math.hypot(dx, dy);
    if (distance === 0) return;

    const duration = (distance / PIECE_MOVE_SPEED) * 1000;
    if (duration <= 0) return;

    moving.setAttribute("x", startX);
    moving.setAttribute("y", startY);

    let startTime = null;
    const step = (now) => {
        if (startTime === null) startTime = now;
        const t = Math.min(1, (now - startTime) / duration);
        const curX = startX + dx * t;
        const curY = startY + dy * t;
        moving.setAttribute("x", curX);
        moving.setAttribute("y", curY);
        if (t < 1) requestAnimationFrame(step);
    };
    requestAnimationFrame(step);
}


// ====== 点击逻辑 ======
function onBoardClick(evt) {
    if (isGameOver()) return;

    const sq = svgPointToSquare(evt);
    if (sq === null) return;

    // 如果已经选中了一个棋子，先看是不是点到它的走法终点
    if (selectedSq !== null) {
        const mv = movesFromSelected.find(m => m.to === sq);
        if (mv) {
            playMove(mv);
            return;
        }
    }

    // 没点到可走终点 => 重新选择棋子 / 取消选择
    const info = board[sq];
    if (!info) {
        // 空格 => 取消选中
        selectedSq = null;
        movesFromSelected = [];
        renderBoard();
        return;
    }

    // 只允许选当前行棋方的棋子
    const pieceSide = info.side; // 0 红, 1 黑
    if (pieceSide !== sideToMove) {
        selectedSq = null;
        movesFromSelected = [];
        renderBoard();
        return;
    }

    selectedSq = sq;
    movesFromSelected = legalMoves.filter(m => m.from === sq);
    renderBoard();
}

// ====== HTTP 交互：尝试恢复已有对局 ======
async function tryResumeGame() {
    const savedId = getCookie("xionghan_game_id");
    if (!savedId) {
        // 没有存过对局，直接开新局
        await newGame();
        return;
    }

    try {
        const res = await fetch("/api/state", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ game_id: savedId })
        });

        if (!res.ok) {
            console.warn("resume failed, start new game", res.status);
            deleteCookie("xionhan_game_id"); // 名字拼错也顺便清一次
            deleteCookie("xionghan_game_id");
            await newGame();
            return;
        }

        const data = await res.json();
        gameId = savedId;
        legalMoves = data.legal_moves || [];
        updateBoardFromFen(data.position);
        selectedSq = null;
        movesFromSelected = [];
        lastMove = null;
        loadMoveCountFromSession();
        renderBoard();
    } catch (e) {
        console.error("resume error, start new game", e);
        await newGame();
    }
}


// ====== HTTP 交互 ======
async function newGame() {
    try {
        const res = await fetch("/api/new_game", {
            method: "POST"
        });
        if (!res.ok) {
            console.error("new_game failed", res.status);
            return;
        }
        const data = await res.json();
        gameId = data.game_id;
        // 在这里保存到 session cookie，刷新页面还能读出来

        setSessionCookie("xionghan_game_id", gameId);

        legalMoves = data.legal_moves || [];
        updateBoardFromFen(data.position);
        selectedSq = null;
        movesFromSelected = [];
        lastMove = null;
        resetMoveCount();
        renderBoard();
    } catch (e) {
        console.error("new_game error", e);
    }
}


async function playMove(mv) {
    if (!gameId) return;
    try {
        const res = await fetch("/api/play", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
                game_id: gameId,
                move: mv
            })
        });
        if (!res.ok) {
            const errText = (await res.text()).trim();
            if (res.status === 404 && errText === "game not found") {
                console.warn("game not found, recreating game");
                await newGame();
                return;
            }
            let tip = `走棋失败: ${errText || ("HTTP " + res.status)}`;
            if (res.status === 400 && errText === "repetition_forbidden") {
                tip = "长将提醒：重复局面已达 3 次，这步已被禁止，请换一步。";
            }
            const statusEl = document.getElementById("gameStatus");
            if (statusEl) statusEl.innerText = tip;
            alert(tip);
            console.error("play failed", res.status, errText);
            return;
        }
        const data = await res.json();
        // 后端 PlayResponse：{ position, to_move, legal_moves, status }
        legalMoves = data.legal_moves || [];
        updateBoardFromFen(data.position);
        selectedSq = null;
        movesFromSelected = [];
        lastMove = mv; // 记录上一步走子
        moveCount += 1;
        saveMoveCountToSession();
        updateMoveCountUI();
        renderBoard(mv);
        // 你可以看 data.status 判断是否将死/和棋
    } catch (e) {
        console.error("play error", e);
    }
}

async function requestAiMove() {
    if (isGameOver()) return;
    if (!currentFen) return;
    try {
        const btn = document.getElementById("btnAiMove");
        const originalText = btn.innerText;
        btn.innerText = "Thinking...";
        btn.disabled = true;

        const res = await fetch("/api/ai_move", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
                game_id: gameId,
                position: expandFen(currentFen),
                to_move: sideToMove,
                max_depth: 2,
                time_ms: 5000
            })
        });

        btn.innerText = originalText;
        btn.disabled = false;

        if (!res.ok) {
            const errText = (await res.text()).trim();
            if (res.status === 404 && errText === "game not found") {
                console.warn("game not found on ai_move, recreating game");
                await newGame();
                return;
            }
            console.error("ai_move failed", res.status, errText);
            return;
        }

        const data = await res.json();
        
        // 更新胜率和搜索信息
        updateUiStats(data);

        if (data.status !== "ok") {
            console.warn("AI no move:", data.status);
            return;
        }

        await playMove(data.best_move);

    } catch (e) {
        console.error("ai_move error", e);
        const btn = document.getElementById("btnAiMove");
        btn.innerText = "AI Move";
        btn.disabled = false;
    }
}

function updateUiStats(data) {
    if (data.win_prob !== undefined) {
        const winPct = (data.win_prob * 100).toFixed(1);
        const textEl = document.getElementById("winProbText");
        const barEl = document.getElementById("winBarFill");
        if (textEl) textEl.innerText = winPct + "%";
        if (barEl) barEl.style.width = winPct + "%";
    }
    
    if (data.depth !== undefined && data.nodes !== undefined) {
        const nodesStr = data.nodes > 1000 ? (data.nodes / 1000).toFixed(1) + "k" : data.nodes;
        const infoEl = document.getElementById("searchInfo");
        if (infoEl) infoEl.innerText = `${data.depth} / ${nodesStr}`;
    }
}


// ====== 启动 ======
document.addEventListener("DOMContentLoaded", () => {

    const boardSvg = document.getElementById("boardSvg");
    boardSvg.addEventListener("click", onBoardClick);

    document.getElementById("btnAiMove")
        .addEventListener("click", requestAiMove);

    document.getElementById("btnNewGame")
        .addEventListener("click", () => {
            if(confirm("Start a new game?")) newGame();
        });

    tryResumeGame();
});
