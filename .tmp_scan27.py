#!/usr/bin/env python3
"""Cycle-27 sweep: container and text METHOD surfaces the earlier sweeps did not cover.
Every case is a one-file program; we run CPython, --interp and --aot, and report the exit-0
wrong answers first because those are the ladder's top priority."""
import subprocess, sys, os, tempfile

BIN = sys.argv[1] if len(sys.argv) > 1 else "./build/pyre"

CASES = [
    # list methods the print road and the operation road may disagree about
    'xs = [3, 1, 2]\nxs.sort()\nprint(xs)',
    'xs = [3, 1, 2]\nprint(xs)',
    'xs = [1, 2, 3]\nxs.reverse()\nprint(xs)',
    'xs = [1, 2]\nxs.insert(0, 9)\nprint(xs)',
    'xs = [1, 2, 3]\nxs.remove(2)\nprint(xs)',
    'print([1, 2, 1].count(1))',
    'print([1, 2, 1].index(2))',
    'xs = [1, 2]\nprint(xs.pop())\nprint(xs)',
    'xs = [1, 2]\nprint(xs.pop(0))\nprint(xs)',
    'print([1, [2, 3]].copy())',
    'xs = [1, 2]\nxs.extend([3])\nprint(xs)',
    'print(sorted([3, 1, 2], reverse=True))',
    'print(sum([1, 2, 3], 10))',
    # dict methods
    'print({"a": 1}.get("a", 5))',
    'print({"a": 1}.pop("a"))',
    'print({"a": 1}.pop("z", 9))',
    'print({"a": 1}.setdefault("b", 2))',
    'print({"a": 1}.update({"b": 2}))',
    'd = {"a": 1}\nd.update({"b": 2})\nprint(d)',
    'print({"a": 1}.clear())',
    'print({"b": 2, "a": 1} < {"a": 1})',
    'print(len({"a": 1, "b": 2}))',
    'print({"a": [1, 2]}["a"][1])',
    # set methods
    'print({1, 2}.union({3}))',
    'print({1, 2}.intersection({2, 3}))',
    'print({1, 2}.difference({1}))',
    'print({1, 2}.issubset({1, 2, 3}))',
    'print({1, 2}.add(3))',
    's = {1}\ns.add(2)\nprint(s)',
    's = {1, 2}\ns.discard(1)\nprint(s)',
    # text methods not previously swept
    'print("a,b,,c".split(","))',
    'print("a b".split())',
    'print(",".join(["a", "b"]))',
    'print("a,b".split(",", 1))',
    'print("abcdef"[1:3])',
    'print("abcdef"[::2])',
    'print("abcdef"[::-1])',
    'print("Hello".swapcase())',
    'print("abc".casefold())',
    'print("a1b2".isdigit())',
    'print("a1".isalnum())',
    'print("  a".lstrip())',
    'print("a  ".rstrip())',
    'print("a\\tb".expandtabs())',
    'print("ab".center(6, "-"))',
    'print("%s-%d" % ("a", 3))',
    'print("a{}b".format(1))',
    'print("abc".replace("b", "X"))',
    'print("aBc".startswith("a"))',
    'print("aaa".count("a"))',
    'print("abc".encode())',
    'print(str([1, "a"]))',
    'print(repr([1, "a"]))',
    'print(str({"a": 1}))',
    'print(str((1, 2)))',
    'print(bool({}))',
    'print(bool("x"))',
    # numeric builtins
    'print(abs(-2.5))',
    'print(divmod(7, 2))',
    'print(pow(2, 10))',
    'print(round(2.675, 2))',
    'print(round(1500, -2))',
    'print(int("42"))',
    'print(int("42", 16))',
    'print(float("1.5"))',
    'print(hex(255), oct(8), bin(5))',
    'print(sum([1, 2, 3], start=10))',
    'print(max([1, 2], key=abs))',
    'print(any([]), all([]))',
    'print(list(range(3, 0, -1)))',
    'print(list(range(0, 10, 3)))',
    'print(len(range(5)))',
    'print(1 in {1, 2})',
    'print(1 in [1, 2])',
    'print("a" in {"a": 1})',
]

def run(cmd):
    try:
        r = subprocess.run(cmd, capture_output=True, text=True, timeout=25)
        return (r.stdout + r.stderr).replace("\r", ""), r.returncode
    except subprocess.TimeoutExpired:
        return "<timeout>", -1

os.makedirs("/tmp/sweep27", exist_ok=True)
bad = 0
for i, src in enumerate(CASES):
    f = f"/tmp/sweep27/c{i}.gy"
    open(f, "w").write(src + "\n")
    py, pyc = run(["python3", f])
    it, itc = run([BIN, "--interp", "--file", f])
    ao, aoc = run([BIN, "--aot", "--file", f])
    ref = py.replace("\r", "")
    # a wrong answer at exit 0 is the loud class
    for label, out, code in (("--interp", it, itc), ("--aot", ao, aoc)):
        if pyc == 0 and code == 0 and out != ref:
            print(f"#{i:02d} [{label}] WRONG AT EXIT 0  {src!r}")
            print(f"    py: {ref!r}")
            print(f"    we: {out!r}")
            bad += 1
        elif code == 2:
            print(f"#{i:02d} [{label}] EXIT 2  {src!r}")
            print(f"    {out.strip().splitlines()[-1][:110] if out.strip() else '(silent)'}")
            bad += 1
        elif pyc == 0 and code == 3 and pyc == 0 and "Error" not in out:
            pass
print(f"--- {bad} loud findings over {len(CASES)} programs")
