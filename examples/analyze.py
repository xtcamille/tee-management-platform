#!/usr/bin/env python3
"""学生数据统计分析脚本"""

import argparse
import csv
import sys
from collections import Counter


def load_data(filepath):
    with open(filepath, encoding="utf-8") as f:
        return list(csv.DictReader(f))


def analyze(rows):
    total = len(rows)
    classes = Counter(r["class"] for r in rows)
    majors = Counter(r["major"] for r in rows)
    genders = Counter(r["gender"] for r in rows)
    ages = [int(r["age"]) for r in rows]
    avg_age = sum(ages) / total

    lines = []
    lines.append("=" * 40)
    lines.append(f"{'学生数据统计报告':^32}")
    lines.append("=" * 40)

    lines.append(f"\n总人数: {total}")
    lines.append(f"\n平均年龄: {avg_age:.1f}")

    lines.append("\n各班级人数:")
    for cls in sorted(classes):
        lines.append(f"  {cls}: {classes[cls]}人")

    lines.append("\n各专业人数:")
    for major in sorted(majors):
        lines.append(f"  {major}: {majors[major]}人")

    lines.append("\n性别比例:")
    for gender in sorted(genders):
        pct = genders[gender] / total * 100
        lines.append(f"  {gender}: {genders[gender]}人 ({pct:.1f}%)")

    lines.append("=" * 40)
    return "\n".join(lines)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="学生数据统计分析")
    parser.add_argument("file", nargs="?", default="students.csv", help="CSV 文件路径")
    parser.add_argument("-o", "--output", help="结果输出文件路径")
    args = parser.parse_args()

    rows = load_data(args.file)
    report = analyze(rows)
    print(report)

    if args.output:
        with open(args.output, "w", encoding="utf-8") as f:
            f.write(report + "\n")
        print(f"\n结果已保存至: {args.output}")
