// 自对弈逻辑控制
let isRunning = false;
let isPaused = false;

const logEl = document.getElementById('log');
function addLog(msg) {
    const div = document.createElement('div');
    div.style.borderBottom = "1px solid #222";
    div.style.padding = "2px 0";
    div.innerText = `[${new Date().toLocaleTimeString()}] ${msg}`;
    logEl.prepend(div);
}

async function startSelfPlay() {
    if (isRunning) return;
    
    addLog("正在初始化新对局...");
    await newGame(); // 调用 main.js 中的全局函数
    
    isRunning = true;
    isPaused = false;
    document.getElementById('btnPause').innerText = "暂停对弈";
    updateControls();
    
    runLoop();
}

async function runLoop() {
    while (isRunning) {
        if (isPaused) {
            await new Promise(r => setTimeout(r, 500));
            continue;
        }

        if (isGameOver()) {
            addLog("游戏结束！");
            isRunning = false;
            updateControls();
            break;
        }

        const sideName = sideToMove === 0 ? "红方" : "黑方";
        const prefix = sideToMove === 0 ? "red" : "black";
        
        const algo = document.getElementById(prefix + 'Algo').value;
        const val = parseInt(document.getElementById(prefix + 'Val').value, 10);
        const delay = parseInt(document.getElementById('stepDelay').value, 10);

        const sideEl = document.getElementById('sideToMove');
        if (sideEl) {
            sideEl.innerText = sideName;
            sideEl.style.color = sideToMove === 0 ? '#f55' : '#5f5';
        }

        addLog(`${sideName} 思考中 (${algo.toUpperCase()} val=${val})...`);

        try {
            const res = await fetch("/api/ai_move", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({
                    game_id: gameId,
                    position: expandFen(currentFen),
                    to_move: sideToMove,
                    max_depth: algo === 'ab' ? val : 1,
                    mcts_simulations: algo === 'mcts' ? val : 0,
                    use_mcts: algo === 'mcts',
                    time_ms: 60000 // 最大允许 60s
                })
            });

            if (!isRunning) break; // 防止在等待 AI 时点击了终止

            if (!res.ok) throw new Error("AI 接口返回错误: " + res.status);
            const data = await res.json();
            
            if (data.status !== "ok") {
                addLog(`警告: ${data.status}`);
                if (data.status === "no_moves") {
                    addLog("无棋可走，对局结束。");
                    isRunning = false;
                    updateControls();
                    break;
                }
            } else {
                // 执行落子
                addLog(`${sideName} 走子: ${data.best_move.from} -> ${data.best_move.to}`);
                await playMove(data.best_move); // 调用 main.js 中的函数
                
                // 实时更新 UI 状态 (如果 main.js 里没有自动更新的话)
                if (typeof updateUiStats === 'function') {
                    updateUiStats(data);
                }
            }

            // 等待设定间隔，以便人类观察
            await new Promise(r => setTimeout(r, delay));

        } catch (e) {
            addLog(`异常中止: ${e.message}`);
            isRunning = false;
            updateControls();
            break;
        }
    }
}

function pauseSelfPlay() {
    isPaused = !isPaused;
    document.getElementById('btnPause').innerText = isPaused ? "继续对弈" : "暂停对弈";
    addLog(isPaused ? "已暂停" : "已继续");
}

function stopSelfPlay() {
    isRunning = false;
    isPaused = false;
    updateControls();
    addLog("对弈已强制终止。");
}

function updateControls() {
    document.getElementById('btnStart').disabled = isRunning;
    document.getElementById('btnPause').disabled = !isRunning;
    document.getElementById('btnStop').disabled = !isRunning;
    
    // 运行期间锁定算法配置
    ['redAlgo', 'redVal', 'blackAlgo', 'blackVal', 'stepDelay'].forEach(id => {
        const el = document.getElementById(id);
        if (el) el.disabled = isRunning;
    });
}

// 绑定按钮
document.getElementById('btnStart').onclick = startSelfPlay;
document.getElementById('btnPause').onclick = pauseSelfPlay;
document.getElementById('btnStop').onclick = stopSelfPlay;
