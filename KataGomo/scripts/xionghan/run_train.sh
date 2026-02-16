#!/bin/bash
# Master training loop for Xionghan Xiangqi - Optimized for 4*H20

mkdir -p data/
mkdir -p data/selfplay/
mkdir -p data/models/
mkdir -p data/shuffleddata/
mkdir -p data/tmp/

# 已经同步到容器内的 TensorRT 路径
TRT_LIB="/tmp/testcgo/TensorRT-10.15.1.29/lib"
PY_CUDA_LIB="/usr/local/lib/python3.12/dist-packages/nvidia/cublas/lib/"
# 这里的 GO_BRIDGE_LIB 我们用相对于 run_train.sh 的路径
GO_BRIDGE_LIB="$(pwd)/../../cpp/lib"

export LD_LIBRARY_PATH=$TRT_LIB:$PY_CUDA_LIB:$GO_BRIDGE_LIB:/usr/local/cuda/lib64:/usr/local/cuda-12/lib64:$LD_LIBRARY_PATH

# 绝对路径引用 katago
KATAGO_BIN="/tmp/testcgo/KataGomo/cpp/build/katago"

# 模型配置：针对 H20 算力升级模型
MODEL_KIND="b15c256"
MODEL_DESC="b15c256-fson-mish-rvglr-bnh"

# 进入 Python 脚本目录执行后续操作
cd train

while true
do
    echo "--- Starting Selfplay (5,000 Games) ---"
    # 1. 自博弈产生数据 - 调整为 5000 局以加快迭代速度
    $KATAGO_BIN selfplay -models-dir ../data/models -config ../selfplay.cfg -output-dir ../data/selfplay -max-games-total 5000
    
    # 检查是否有数据产生
    if [ ! "$(ls -A ../data/selfplay/)" ]; then
        echo "Error: No selfplay data generated! Check for library or model errors."
        exit 1
    fi

    echo "--- Starting Shuffle ---"
    # 2. 数据洗牌 - 增加并行度
    bash shuffle.sh ../data ../data/tmp 32 1024
    
    echo "--- Starting Training (2*4090 DDP) ---"
    # 3. 训练模型 (4090 显卡 0,1)
    bash train.sh ../data $MODEL_KIND $MODEL_DESC 1024 main \
      -samples-per-epoch 1000000 \
      -pos-len 13 \
      -multi-gpus 0,1
    
    echo "--- Exporting Model ---"
    # 4. 导出模型供下一轮使用
    python3 export_model_pytorch.py \
      -checkpoint ../data/train/$MODEL_KIND/checkpoint.ckpt \
      -export-dir ../data/models/ \
      -model-name xionghan_gen \
      -filename-prefix model_$(date +%s) \
      -pos-len 13
done
