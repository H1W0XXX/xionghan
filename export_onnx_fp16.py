import argparse
import os
import sys

import torch

# Ensure KataGomo python path is available.
sys.path.append(os.path.join(os.getcwd(), "KataGomo", "python"))
from load_model import load_model


def parse_args():
    parser = argparse.ArgumentParser(description="Export Xionghan ONNX in fp16/fp32.")
    parser.add_argument(
        "--checkpoint",
        default="KataGomo/scripts/xionghan/data/train/b15c256/checkpoint.ckpt",
        help="Path to checkpoint.ckpt",
    )
    parser.add_argument(
        "--output",
        default="xionghan-fp16.onnx",
        help="Output ONNX file path",
    )
    parser.add_argument(
        "--pos-len",
        type=int,
        default=13,
        help="Board size used by the model",
    )
    parser.add_argument(
        "--precision",
        choices=["fp16", "fp32"],
        default="fp16",
        help="Export precision",
    )
    parser.add_argument(
        "--use-swa",
        action="store_true",
        help="Use SWA model if present in checkpoint",
    )
    parser.add_argument(
        "--fixed-batch",
        action="store_true",
        help="Export fixed batch=1 instead of dynamic batch axis",
    )
    return parser.parse_args()


class ExportWrapper(torch.nn.Module):
    def __init__(self, model):
        super().__init__()
        self.model = model

    def forward(self, x, g):
        outputs = self.model(x, g)
        policy = outputs[0][0][:, 0, :]
        value = outputs[0][1]
        return policy, value


def main():
    args = parse_args()
    print(f"Loading checkpoint: {args.checkpoint}")

    device = "cuda" if torch.cuda.is_available() else "cpu"
    
    model, swa_model, _ = load_model(
        args.checkpoint,
        use_swa=args.use_swa,
        device=device,
        pos_len=args.pos_len,
        verbose=False,
    )
    target_model = swa_model.module if (args.use_swa and swa_model is not None) else model
    target_model.eval()

    wrapper = ExportWrapper(target_model)
    wrapper.eval()
    wrapper.to(device)

    # 架构：如果要求 fp16 且有显卡 CUDA，在 PyTorch 层转半精度；若是 CPU 则保持 fp32 进行追踪
    trace_fp16 = (args.precision == "fp16" and device == "cuda")
    if trace_fp16:
        wrapper = wrapper.half()

    total_params = sum(p.numel() for p in target_model.parameters())
    print(f"Model loaded. Total parameters: {total_params}")
    print(f"Export precision: {args.precision} on {device} (Tracing in {'fp16' if trace_fp16 else 'fp32'})")

    trace_dtype = torch.float16 if trace_fp16 else torch.float32
    
    dummy_x = torch.randn(1, 25, args.pos_len, args.pos_len, dtype=trace_dtype, device=device)
    dummy_g = torch.randn(1, 19, dtype=trace_dtype, device=device)

    dynamic_axes = None
    if not args.fixed_batch:
        dynamic_axes = {
            "bin_inputs": {0: "batch_size"},
            "global_inputs": {0: "batch_size"},
            "policy": {0: "batch_size"},
            "value": {0: "batch_size"},
        }

    print(f"Exporting ONNX -> {args.output}")
    with torch.no_grad():
        torch.onnx.export(
            wrapper,
            (dummy_x, dummy_g),
            args.output,
            export_params=True,
            opset_version=13,
            do_constant_folding=True,
            input_names=["bin_inputs", "global_inputs"],
            output_names=["policy", "value"],
            dynamic_axes=dynamic_axes,
        )

    # 架构：在 CPU 环境下，将刚才导出的 fp32 ONNX 离线转换为 fp16 并覆盖保存
    if args.precision == "fp16" and device == "cpu":
        print("CPU environment detected. Converting fp32 ONNX to fp16 via onnxconverter_common...")
        try:
            import onnx
            from onnxconverter_common import float16
            
            onnx_model = onnx.load(args.output)
            onnx_model_fp16 = float16.convert_float_to_float16(onnx_model)
            onnx.save(onnx_model_fp16, args.output)
            print("ONNX fp16 conversion successful.")
        except ImportError:
            print("ERROR: Missing required libraries for CPU fp16 conversion.")
            print("Please run: pip install onnx onnxconverter-common")
            sys.exit(1)

    size = os.path.getsize(args.output)
    print(f"SUCCESS: {args.output} ({size / 1024 / 1024:.2f} MB)")
    if size < 5 * 1024 * 1024:
        print("WARNING: ONNX file is unexpectedly small.")


if __name__ == "__main__":
    main()