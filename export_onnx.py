import os
import torch
import sys

# 确保路径包含 KataGomo/python
sys.path.append(os.path.join(os.getcwd(), "KataGomo", "python"))
from load_model import load_model

def main():
    CHECKPOINT_PATH = "KataGomo/scripts/xionghan/data/train/b15c256/checkpoint.ckpt"
    POS_LEN = 13
    
    print(f"Loading checkpoint: {CHECKPOINT_PATH}")
    # 加载模型
    model, swa_model, _ = load_model(CHECKPOINT_PATH, use_swa=False, device="cpu", pos_len=POS_LEN, verbose=False)
    target_model = swa_model.module if swa_model is not None else model
    target_model.eval()

    # 打印参数量，确认加载成功
    total_params = sum(p.numel() for p in target_model.parameters())
    print(f"Model loaded. Total parameters: {total_params}")

    # 使用 Wrapper 保证输出格式对齐 Go 引擎
    class ExportWrapper(torch.nn.Module):
        def __init__(self, m):
            super().__init__()
            self.m = m
        def forward(self, x, g):
            # 获取主分支输出
            outputs = self.m(x, g)
            # outputs[0] 是 main_outputs
            policy = outputs[0][0][:, 0, :] # 提取 policy 通道 0
            value = outputs[0][1]           # 提取 value
            return policy, value

    wrapper = ExportWrapper(target_model)
    wrapper.eval()

    dummy_x = torch.randn(1, 25, 13, 13)
    dummy_g = torch.randn(1, 19)

    print("Exporting to xionghan.onnx using stable tracing...")
    with torch.no_grad():
        torch.onnx.export(
            wrapper,
            (dummy_x, dummy_g),
            "xionghan.onnx",
            export_params=True,
            opset_version=13,
            do_constant_folding=True,
            input_names=['bin_inputs', 'global_inputs'],
            output_names=['policy', 'value'],
            dynamic_axes={
                'bin_inputs': {0: 'batch_size'},
                'global_inputs': {0: 'batch_size'},
                'policy': {0: 'batch_size'},
                'value': {0: 'batch_size'}
            }
        )
    
    size = os.path.getsize("xionghan.onnx")
    print(f"SUCCESS! ONNX file created. Size: {size/1024/1024:.2f} MB")
    if size < 5 * 1024 * 1024:
        print("ERROR: File is still too small. Ensure you are using Torch < 2.5")

if __name__ == "__main__":
    main()
