import os
import torch
import modelconfigs
from model_pytorch import Model
import struct
import numpy as np

# 配置信息
MODEL_KIND = "b10c128-fson-mish-rvglr-bnh"
POS_LEN = 13
OUTPUT_PATH = "KataGomo/scripts/xionghan/data/models/model_0.bin"

# 1. 确保目录存在
os.makedirs(os.path.dirname(OUTPUT_PATH), exist_ok=True)

# 2. 初始化模型
print(f"Initializing model {MODEL_KIND} for {POS_LEN}x{POS_LEN} board...")
config = modelconfigs.config_of_name[MODEL_KIND]
model = Model(config, POS_LEN)
model.initialize()

# 3. 导出为 KataGo .bin 格式
print(f"Exporting model to {OUTPUT_PATH}...")

f = open(OUTPUT_PATH, "wb")

def writeln(s):
    f.write((str(s) + "
").encode("ascii"))

def writestr(s):
    f.write(s.encode("ascii"))

def write_weights(weights):
    reshaped = weights.detach().cpu().numpy().flatten()
    num_weights = len(reshaped)
    writestr("@BIN@")
    f.write(struct.pack(f'<{num_weights}f', *reshaped))
    writestr("
")

# Meta info
writeln("model_0") # model name
writeln(11)        # version
writeln(25)        # num bin features
writeln(19)        # num global features

# Trunk
trunk = model.trunk
# initialConv
write_conv = lambda name, cw: (writeln(name), writeln(cw.shape[2]), writeln(cw.shape[3]), writeln(cw.shape[1]), writeln(cw.shape[0]), writeln(1), writeln(1), write_weights(cw))
write_conv("trunk.initialConv", trunk.initialConv.weight)

# initialMatMul
writeln("trunk.initialMatMul")
writeln(trunk.initialMatMul.weight.shape[1])
writeln(trunk.initialMatMul.weight.shape[0])
write_weights(trunk.initialMatMul.weight)

# Blocks
writeln(len(trunk.blocks))
for i, block in enumerate(trunk.blocks):
    writeln(f"trunk.block{i}")
    writeln(0) # ORDINARY_BLOCK_KIND
    # preBN
    writeln(f"trunk.block{i}.preBN")
    writeln(block.norm1.num_features)
    writeln(block.norm1.eps)
    writeln(1 if block.norm1.affine else 0)
    writeln(1 if block.norm1.affine else 0)
    write_weights(block.norm1.running_mean)
    write_weights(block.norm1.running_var)
    if block.norm1.affine:
        write_weights(block.norm1.weight)
        write_weights(block.norm1.bias)
    # preActivation
    writeln(f"trunk.block{i}.preActivation")
    writeln(3) # MISH
    # regularConv
    write_conv(f"trunk.block{i}.regularConv", block.conv1.weight)
    # midBN
    writeln(f"trunk.block{i}.midBN")
    writeln(block.norm2.num_features)
    writeln(block.norm2.eps)
    writeln(1 if block.norm2.affine else 0)
    writeln(1 if block.norm2.affine else 0)
    write_weights(block.norm2.running_mean)
    write_weights(block.norm2.running_var)
    if block.norm2.affine:
        write_weights(block.norm2.weight)
        write_weights(block.norm2.bias)
    # midActivation
    writeln(f"trunk.block{i}.midActivation")
    writeln(3) # MISH
    # finalConv
    write_conv(f"trunk.block{i}.finalConv", block.conv2.weight)

# trunkTipBN
writeln("trunk.trunkTipBN")
writeln(trunk.trunkTipNorm.num_features)
writeln(trunk.trunkTipNorm.eps)
writeln(1 if trunk.trunkTipNorm.affine else 0)
writeln(1 if trunk.trunkTipNorm.affine else 0)
write_weights(trunk.trunkTipNorm.running_mean)
write_weights(trunk.trunkTipNorm.running_var)
if trunk.trunkTipNorm.affine:
    write_weights(trunk.trunkTipNorm.weight)
    write_weights(trunk.trunkTipNorm.bias)
writeln("trunk.trunkTipActivation")
writeln(3) # MISH

# Policy Head
ph = model.policy_head
writeln("policyHead")
write_conv("policyHead.p1Conv", ph.p1Conv.weight)
write_conv("policyHead.g1Conv", ph.g1Conv.weight)
writeln("policyHead.g1BN")
writeln(ph.g1Norm.num_features)
writeln(ph.g1Norm.eps)
writeln(1 if ph.g1Norm.affine else 0)
writeln(1 if ph.g1Norm.affine else 0)
write_weights(ph.g1Norm.running_mean)
write_weights(ph.g1Norm.running_var)
if ph.g1Norm.affine:
    write_weights(ph.g1Norm.weight)
    write_weights(ph.g1Norm.bias)
writeln("policyHead.g1Activation")
writeln(3) # MISH
writeln("policyHead.gpoolToBiasMul")
writeln(ph.gpoolToBiasMul.weight.shape[1])
writeln(ph.gpoolToBiasMul.weight.shape[0])
write_weights(ph.gpoolToBiasMul.weight)
writeln("policyHead.p1BN")
writeln(ph.p1Norm.num_features)
writeln(ph.p1Norm.eps)
writeln(1 if ph.p1Norm.affine else 0)
writeln(1 if ph.p1Norm.affine else 0)
write_weights(ph.p1Norm.running_mean)
write_weights(ph.p1Norm.running_var)
if ph.p1Norm.affine:
    write_weights(ph.p1Norm.weight)
    write_weights(ph.p1Norm.bias)
writeln("policyHead.p1Activation")
writeln(3) # MISH
write_conv("policyHead.p2Conv", ph.p2Conv.weight)
writeln("policyHead.gpoolToPassMul")
writeln(ph.gpoolToPassMul.weight.shape[1])
writeln(ph.gpoolToPassMul.weight.shape[0])
write_weights(ph.gpoolToPassMul.weight)

# Value Head
vh = model.value_head
writeln("valueHead")
write_conv("valueHead.v1Conv", vh.v1Conv.weight)
writeln("valueHead.v1BN")
writeln(vh.v1Norm.num_features)
writeln(vh.v1Norm.eps)
writeln(1 if vh.v1Norm.affine else 0)
writeln(1 if vh.v1Norm.affine else 0)
write_weights(vh.v1Norm.running_mean)
write_weights(vh.v1Norm.running_var)
if vh.v1Norm.affine:
    write_weights(vh.v1Norm.weight)
    write_weights(vh.v1Norm.bias)
writeln("valueHead.v1Activation")
writeln(3) # MISH
writeln("valueHead.v2Mul")
writeln(vh.v2Mul.weight.shape[1])
writeln(vh.v2Mul.weight.shape[0])
write_weights(vh.v2Mul.weight)
writeln("valueHead.v2Bias")
writeln(vh.v2Bias.weight.shape[0])
write_weights(vh.v2Bias.weight)
writeln("valueHead.v2Activation")
writeln(3) # MISH
writeln("valueHead.v3Mul")
writeln(vh.v3Mul.weight.shape[1])
writeln(vh.v3Mul.weight.shape[0])
write_weights(vh.v3Mul.weight)
writeln("valueHead.v3Bias")
writeln(vh.v3Bias.weight.shape[0])
write_weights(vh.v3Bias.weight)
writeln("valueHead.sv3Mul")
writeln(vh.sv3Mul.weight.shape[1])
writeln(vh.sv3Mul.weight.shape[0])
write_weights(vh.sv3Mul.weight)
writeln("valueHead.sv3Bias")
writeln(vh.sv3Bias.weight.shape[0])
write_weights(vh.sv3Bias.weight)
write_conv("valueHead.vOwnershipConv", vh.vOwnershipConv.weight)

f.close()
print("Done!")
