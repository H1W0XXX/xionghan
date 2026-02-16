import os
import torch
import modelconfigs
from model_pytorch import Model

# 配置信息 (必须与 train.sh 保持一致)
MODEL_KIND = "b10c128-fson-mish-rvglr-bnh"
POS_LEN = 13
OUTPUT_PATH = "KataGomo/scripts/xionghan/data/models/model_0.ckpt"

# 1. 确保目录存在
os.makedirs(os.path.dirname(OUTPUT_PATH), exist_ok=True)

# 2. 初始化模型
print(f"Initializing model {MODEL_KIND} for {POS_LEN}x{POS_LEN} board...")
config = modelconfigs.config_of_name[MODEL_KIND]
model = Model(config, POS_LEN)
model.initialize() # KataGo models usually need an explicit initialize call

# 3. 保存为 PyTorch Checkpoint (符合 load_model.py 的格式)
print(f"Saving checkpoint to {OUTPUT_PATH}...")
checkpoint = {
    "config": config,      # 必须是 "config"
    "model": model.state_dict(),  # 必须是 "model"
}
torch.save(checkpoint, OUTPUT_PATH)
print("Done!")
