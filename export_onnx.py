import argparse
import os
import sys

import torch

# Ensure KataGomo/python is importable.
sys.path.append(os.path.join(os.getcwd(), "KataGomo", "python"))
from load_model import load_model


def parse_args():
    parser = argparse.ArgumentParser(description="Export single-file dynamic-batch ONNX (fp32).")
    parser.add_argument(
        "--checkpoint",
        default="KataGomo/scripts/xionghan/data/train/b15c256/checkpoint.ckpt",
        help="Path to checkpoint.ckpt",
    )
    parser.add_argument(
        "--output",
        default="xionghan.onnx",
        help="Output ONNX file path",
    )
    parser.add_argument(
        "--pos-len",
        type=int,
        default=13,
        help="Board size used by the model",
    )
    parser.add_argument(
        "--use-swa",
        action="store_true",
        help="Use SWA model if present in checkpoint",
    )
    parser.add_argument(
        "--fixed-batch",
        action="store_true",
        help="Export fixed batch=1 (not recommended for current Go runtime strategy).",
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

    model, swa_model, _ = load_model(
        args.checkpoint,
        use_swa=args.use_swa,
        device="cpu",
        pos_len=args.pos_len,
        verbose=False,
    )
    target_model = swa_model.module if (args.use_swa and swa_model is not None) else model
    target_model.eval()

    total_params = sum(p.numel() for p in target_model.parameters())
    print(f"Model loaded. Total parameters: {total_params}")

    wrapper = ExportWrapper(target_model)
    wrapper.eval()

    # Dynamic-batch export only needs a representative shape.
    dummy_x = torch.randn(1, 25, args.pos_len, args.pos_len)
    dummy_g = torch.randn(1, 19)

    dynamic_axes = None
    if not args.fixed_batch:
        dynamic_axes = {
            "bin_inputs": {0: "batch_size"},
            "global_inputs": {0: "batch_size"},
            "policy": {0: "batch_size"},
            "value": {0: "batch_size"},
        }
        print("Export mode: dynamic batch (recommended for Go runtime buckets 1..512).")
    else:
        print("Export mode: fixed batch=1.")

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

    size = os.path.getsize(args.output)
    print(f"SUCCESS: {args.output} ({size / 1024 / 1024:.2f} MB)")
    if size < 5 * 1024 * 1024:
        print("WARNING: ONNX file is unexpectedly small.")


if __name__ == "__main__":
    main()
