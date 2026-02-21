#!/usr/bin/env python3
import argparse
import json
from pathlib import Path
from typing import Dict, List, Tuple

import matplotlib.pyplot as plt


def load_jsonl(path: Path) -> List[Dict]:
    records: List[Dict] = []
    with path.open("r", encoding="utf-8") as f:
        for i, line in enumerate(f, start=1):
            s = line.strip()
            if not s:
                continue
            try:
                obj = json.loads(s)
            except json.JSONDecodeError:
                # Skip malformed lines instead of failing the whole plot.
                continue
            obj["_line"] = i
            records.append(obj)
    return records


def pick_x(obj: Dict, fallback_index: int, keys: Tuple[str, ...]) -> float:
    for k in keys:
        v = obj.get(k)
        if isinstance(v, (int, float)):
            return float(v)
    return float(fallback_index)


def series(records: List[Dict], key: str, x_keys: Tuple[str, ...]) -> Tuple[List[float], List[float]]:
    xs: List[float] = []
    ys: List[float] = []
    for idx, obj in enumerate(records):
        y = obj.get(key)
        if isinstance(y, (int, float)):
            x = pick_x(obj, idx, x_keys)
            xs.append(x / 1_000_000.0)  # show as millions of samples
            ys.append(float(y))
    return xs, ys


def save_total_loss(train: List[Dict], val: List[Dict], out: Path) -> None:
    train_x_keys = ("nsamp", "wsum", "nsamp_train", "wsum_train")
    val_x_keys = ("nsamp_train", "wsum_train", "nsamp", "wsum")
    tx, ty = series(train, "loss", train_x_keys)
    vx, vy = series(val, "loss", val_x_keys)
    plt.figure(figsize=(11, 6), dpi=150)
    if tx:
        plt.plot(tx, ty, label="train loss", linewidth=1.0)
    if vx:
        plt.plot(vx, vy, label="val loss", linewidth=1.2)
    plt.title("Total Loss (Train vs Val)")
    plt.xlabel("Samples (millions)")
    plt.ylabel("Loss")
    plt.grid(True, alpha=0.25)
    plt.legend()
    plt.tight_layout()
    plt.savefig(out)
    plt.close()


def save_policy_value_loss(train: List[Dict], val: List[Dict], out: Path) -> None:
    train_x_keys = ("nsamp", "wsum", "nsamp_train", "wsum_train")
    val_x_keys = ("nsamp_train", "wsum_train", "nsamp", "wsum")
    plt.figure(figsize=(11, 6), dpi=150)
    for key, label in (("p0loss", "train p0loss"), ("p1loss", "train p1loss"), ("vloss", "train vloss")):
        x, y = series(train, key, train_x_keys)
        if x:
            plt.plot(x, y, label=label, linewidth=1.0)
    for key, label in (("p0loss", "val p0loss"), ("p1loss", "val p1loss"), ("vloss", "val vloss")):
        x, y = series(val, key, val_x_keys)
        if x:
            plt.plot(x, y, label=label, linestyle="--", linewidth=1.2)
    plt.title("Policy/Value Losses")
    plt.xlabel("Samples (millions)")
    plt.ylabel("Loss")
    plt.grid(True, alpha=0.25)
    plt.legend(ncol=2)
    plt.tight_layout()
    plt.savefig(out)
    plt.close()


def save_pacc1(train: List[Dict], val: List[Dict], out: Path) -> None:
    train_x_keys = ("nsamp", "wsum", "nsamp_train", "wsum_train")
    val_x_keys = ("nsamp_train", "wsum_train", "nsamp", "wsum")
    tx, ty = series(train, "pacc1", train_x_keys)
    vx, vy = series(val, "pacc1", val_x_keys)
    plt.figure(figsize=(11, 6), dpi=150)
    if tx:
        plt.plot(tx, ty, label="train pacc1", linewidth=1.0)
    if vx:
        plt.plot(vx, vy, label="val pacc1", linewidth=1.2)
    plt.title("Policy Accuracy pacc1 (Train vs Val)")
    plt.xlabel("Samples (millions)")
    plt.ylabel("pacc1")
    plt.grid(True, alpha=0.25)
    plt.legend()
    plt.tight_layout()
    plt.savefig(out)
    plt.close()


def main() -> None:
    parser = argparse.ArgumentParser(description="Plot training curves from KataGomo metrics JSONL files.")
    parser.add_argument(
        "--train-metrics",
        default="data/train/b15c256/metrics_train.json",
        help="Path to metrics_train.json",
    )
    parser.add_argument(
        "--val-metrics",
        default="data/train/b15c256/metrics_val.json",
        help="Path to metrics_val.json",
    )
    parser.add_argument(
        "--out-dir",
        default="plots",
        help="Output directory for PNG charts",
    )
    args = parser.parse_args()

    train_path = Path(args.train_metrics)
    val_path = Path(args.val_metrics)
    out_dir = Path(args.out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)

    train_records = load_jsonl(train_path)
    val_records = load_jsonl(val_path)

    if not train_records:
        raise RuntimeError(f"No readable records in train metrics: {train_path}")
    if not val_records:
        raise RuntimeError(f"No readable records in val metrics: {val_path}")

    out1 = out_dir / "01_total_loss_train_val.png"
    out2 = out_dir / "02_policy_value_losses.png"
    out3 = out_dir / "03_pacc1_train_val.png"

    save_total_loss(train_records, val_records, out1)
    save_policy_value_loss(train_records, val_records, out2)
    save_pacc1(train_records, val_records, out3)

    print(f"Saved: {out1}")
    print(f"Saved: {out2}")
    print(f"Saved: {out3}")


if __name__ == "__main__":
    main()
