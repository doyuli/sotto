"""Read-only package and process measurements; Python standard library only."""
import argparse
import ctypes as C
import json
import math
from pathlib import Path
import platform
import sys
import time


def package_size(root):
    root = Path(root)
    if not root.is_dir() or root.is_symlink():
        raise ValueError(f"not a package directory: {root}")
    files = []
    for path in sorted(root.rglob("*")):
        if path.is_symlink():
            raise ValueError(f"symlink requires explicit accounting: {path}")
        if path.is_file():
            files.append({"path": path.relative_to(root).as_posix(), "bytes": path.stat().st_size})
    if not files:
        raise ValueError(f"empty package: {root}")
    return {"path": str(root), "total_bytes": sum(f["bytes"] for f in files), "files": files}


class MacReader:
    class Usage(C.Structure):
        # sys/resource.h rusage_info_v2; CPU counters use Mach absolute ticks.
        _fields_ = [("uuid", C.c_uint8 * 16)] + [(name, C.c_uint64) for name in (
            "user", "system", "package_wakeups", "interrupt_wakeups", "pageins",
            "wired", "resident", "footprint", "start", "exit", "child_user",
            "child_system", "child_package_wakeups", "child_interrupt_wakeups",
            "child_pageins", "child_elapsed", "disk_read", "disk_written")]

    def __init__(self):
        self.lib = C.CDLL("/usr/lib/libproc.dylib", use_errno=True)
        self.lib.proc_pid_rusage.argtypes = [C.c_int, C.c_int, C.c_void_p]
        self.lib.proc_pid_rusage.restype = C.c_int
        system = C.CDLL("/usr/lib/libSystem.B.dylib")
        system.mach_timebase_info.argtypes = [C.c_void_p]
        system.mach_timebase_info.restype = C.c_int
        timebase = (C.c_uint32 * 2)()
        if system.mach_timebase_info(C.byref(timebase)) != 0 or not timebase[1]:
            raise OSError("mach_timebase_info failed")
        self.numerator, self.denominator = timebase

    def read(self, pid):
        value = self.Usage()
        if self.lib.proc_pid_rusage(pid, 2, C.byref(value)) != 0:
            raise OSError(C.get_errno(), "proc_pid_rusage")
        if value.exit:
            raise ProcessLookupError("process exited")
        return {"start_id": value.start, "cpu_ns": (value.user + value.system) * self.numerator // self.denominator,
                "rss_bytes": value.resident, "physical_footprint_bytes": value.footprint,
                "package_idle_wakeups": value.package_wakeups,
                "interrupt_wakeups": value.interrupt_wakeups}


class WindowsReader:
    class Counters(C.Structure):
        _fields_ = [("cb", C.c_uint32), ("faults", C.c_uint32)] + [
            (name, C.c_size_t) for name in ("peak_ws", "ws", "peak_paged", "paged",
            "peak_nonpaged", "nonpaged", "pagefile", "peak_pagefile", "private")]

    def __init__(self):
        self.kernel = C.WinDLL("kernel32", use_last_error=True)
        self.psapi = C.WinDLL("psapi", use_last_error=True)
        signatures = [
            (self.kernel.OpenProcess, [C.c_uint32, C.c_int, C.c_uint32], C.c_void_p),
            (self.kernel.CloseHandle, [C.c_void_p], C.c_int),
            (self.kernel.GetProcessTimes, [C.c_void_p] + [C.c_void_p] * 4, C.c_int),
            (self.kernel.GetProcessHandleCount, [C.c_void_p, C.c_void_p], C.c_int),
            (self.psapi.GetProcessMemoryInfo, [C.c_void_p, C.c_void_p, C.c_uint32], C.c_int),
        ]
        for function, args, result in signatures:
            function.argtypes, function.restype = args, result

    @staticmethod
    def check(value):
        if not value:
            raise C.WinError(C.get_last_error())
        return value

    def read(self, pid):
        # PROCESS_QUERY_INFORMATION | PROCESS_VM_READ; no write/terminate rights.
        handle = self.check(self.kernel.OpenProcess(0x400 | 0x10, False, pid))
        try:
            created, exited, kernel, user = (C.c_uint64() for _ in range(4))
            self.check(self.kernel.GetProcessTimes(handle, C.byref(created), C.byref(exited),
                                                   C.byref(kernel), C.byref(user)))
            if exited.value:
                raise ProcessLookupError("process exited")
            counters, handles = self.Counters(), C.c_uint32()
            counters.cb = C.sizeof(counters)
            self.check(self.psapi.GetProcessMemoryInfo(handle, C.byref(counters), counters.cb))
            self.check(self.kernel.GetProcessHandleCount(handle, C.byref(handles)))
            return {"start_id": created.value, "cpu_ns": (kernel.value + user.value) * 100,
                    "working_set_bytes": counters.ws, "private_bytes": counters.private,
                    "peak_working_set_bytes": counters.peak_ws, "handles": handles.value}
        finally:
            self.kernel.CloseHandle(handle)


def native_reader():
    if sys.platform == "darwin":
        return MacReader()
    if sys.platform == "win32":
        return WindowsReader()
    raise ValueError("process sampling supports Windows and macOS")


class Sampler:
    def __init__(self, read):
        self.read, self.previous = read, {}

    def sample(self, pid, now_ns):
        value = self.read(pid)
        old = self.previous.get(pid)
        if old and old[1]["start_id"] != value["start_id"]:
            raise ProcessLookupError(f"PID {pid} was reused; start a new recording")
        cpu = None
        if old and now_ns > old[0]:
            cpu = 100 * (value["cpu_ns"] - old[1]["cpu_ns"]) / (now_ns - old[0])
        self.previous[pid] = now_ns, value
        return {"pid": pid, "cpu_percent_one_core": cpu, **value}


def positive(value):
    number = float(value)
    if not math.isfinite(number) or number <= 0:
        raise argparse.ArgumentTypeError("must be finite and positive")
    return number


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    sizes = sub.add_parser("sizes", help="uncompressed per-file logical byte sizes")
    sizes.add_argument("directories", nargs="+")
    sample = sub.add_parser("sample", help="JSONL counters for explicit PIDs; never launches or stops apps")
    sample.add_argument("--pid", type=int, action="append", required=True)
    sample.add_argument("--duration", type=positive, default=60)
    sample.add_argument("--interval", type=positive, default=1)
    sample.add_argument("--label", default="manual")
    args = parser.parse_args()
    metadata = {"schema": 1, "platform": platform.platform(), "architecture": platform.machine()}
    if args.command == "sizes":
        print(json.dumps({**metadata, "packages": [package_size(p) for p in args.directories]}, indent=2))
        return
    if any(pid <= 0 or pid > 0x7fffffff for pid in args.pid):
        parser.error("PIDs must be positive signed 32-bit integers")
    sampler = Sampler(native_reader().read)
    start = time.monotonic_ns()
    while True:
        now = time.monotonic_ns()
        values = [sampler.sample(pid, now) for pid in dict.fromkeys(args.pid)]
        print(json.dumps({**metadata, "label": args.label, "elapsed_seconds": (now-start)/1e9,
                          "unix_time_ns": time.time_ns(), "processes": values}), flush=True)
        remaining = args.duration - (time.monotonic_ns() - start) / 1e9
        if remaining <= 0:
            return
        time.sleep(min(args.interval, remaining))


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError) as error:
        sys.exit(str(error))
