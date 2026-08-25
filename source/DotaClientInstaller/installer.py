#!/usr/bin/env python3

import json
import os
import re
import select
import shutil
import subprocess
import time
from collections import deque
from datetime import datetime, timezone
from pathlib import Path


APP_ID = "570"
INSTALL_DIR = Path(os.environ.get("DOTA_INSTALL_DIR", "/dota"))
STATUS_DIR = Path(os.environ.get("DOTA_STATUS_DIR", "/status"))
STATUS_FILE = STATUS_DIR / "status.json"
LOG_FILE = STATUS_DIR / "steamcmd.log"
EXPECTED_INSTALL_BYTES = int(os.environ.get("DOTA_EXPECTED_INSTALL_BYTES", "90000000000"))
CREDENTIALS_FILE = Path(
    os.environ.get("DOTA_CREDENTIALS_FILE", "/run/secrets/dota_client_credentials")
)
PROGRESS_PATTERN = re.compile(
    r"Update state \((?P<state>[^)]+)\)\s+([^,]+),\s+progress:\s+"
    r"(?P<percent>[0-9.]+)\s+\((?P<done>\d+)\s*/\s*(?P<total>\d+)\)",
    re.IGNORECASE,
)
ANSI_PATTERN = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]")
BUILD_PATTERN = re.compile(r'"buildid"\s+"(?P<build>\d+)"')
USEFUL_PATTERNS = (
    "Update state",
    "Success!",
    "ERROR!",
    "FAILED",
    "Downloading",
    "Verifying",
    "Installing",
    "Loading Steam API",
    "Connecting anonymously",
    "Waiting for client config",
    "Waiting for user info",
    "Steam Guard",
    "Invalid Password",
)


def utc_now():
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def directory_size(path):
    if not path.exists():
        return 0

    try:
        result = subprocess.run(
            ["du", "-sb", str(path)],
            check=True,
            capture_output=True,
            text=True,
            timeout=30,
        )
        return int(result.stdout.split()[0])
    except (OSError, ValueError, subprocess.SubprocessError):
        return 0


def build_id():
    manifest = INSTALL_DIR / "steamapps" / f"appmanifest_{APP_ID}.acf"
    try:
        match = BUILD_PATTERN.search(manifest.read_text(encoding="utf-8", errors="replace"))
        return match.group("build") if match else None
    except OSError:
        return None


def atomic_write(payload):
    STATUS_DIR.mkdir(parents=True, exist_ok=True)
    temporary = STATUS_FILE.with_suffix(".json.tmp")
    temporary.write_text(
        json.dumps(payload, ensure_ascii=False, separators=(",", ":")),
        encoding="utf-8",
    )
    os.replace(temporary, STATUS_FILE)


def append_log(line):
    STATUS_DIR.mkdir(parents=True, exist_ok=True)
    if LOG_FILE.exists() and LOG_FILE.stat().st_size > 2 * 1024 * 1024:
        rotated = LOG_FILE.with_suffix(".log.1")
        rotated.unlink(missing_ok=True)
        LOG_FILE.replace(rotated)

    with LOG_FILE.open("a", encoding="utf-8") as handle:
        handle.write(f"{utc_now()} {line}\n")


def clean_line(raw):
    line = ANSI_PATTERN.sub("", raw).strip()
    line = re.sub(r"\s+", " ", line)
    return line[:500]


def steamcmd_quote(value):
    if "\n" in value or "\r" in value:
        raise ValueError("Steam credential contains an unsupported line break.")
    return '"' + value.replace("\\", "\\\\").replace('"', '\\"') + '"'


def load_credentials():
    if not CREDENTIALS_FILE.is_file():
        return None

    data = json.loads(CREDENTIALS_FILE.read_text(encoding="utf-8"))
    username = str(data.get("username") or "").strip()
    password = str(data.get("password") or "")
    guard_code = str(data.get("twoFactorCode") or data.get("authCode") or "").strip()
    if not username or not password:
        raise ValueError("Steam credentials file is incomplete.")
    return username, password, guard_code


def create_run_script(credentials):
    script_file = Path("/tmp/dota-client-install.txt")
    if credentials:
        username, password, guard_code = credentials
        login = f"login {steamcmd_quote(username)} {steamcmd_quote(password)}"
        if guard_code:
            login += f" {steamcmd_quote(guard_code)}"
    else:
        login = "login anonymous"

    script_file.write_text(
        "\n".join(
            [
                "@ShutdownOnFailedCommand 1",
                f"force_install_dir {steamcmd_quote(str(INSTALL_DIR))}",
                login,
                f"app_update {APP_ID} validate",
                "quit",
                "",
            ]
        ),
        encoding="utf-8",
    )
    script_file.chmod(0o600)
    return script_file


def phase_from_line(line):
    lowered = line.lower()
    if "downloading" in lowered:
        return "downloading"
    if "preallocating" in lowered:
        return "preparing"
    if "staging" in lowered or "committing" in lowered:
        return "installing"
    if "verifying" in lowered:
        return "verifying"
    if "reconfiguring" in lowered:
        return "preparing"
    return "installing"


def main():
    INSTALL_DIR.mkdir(parents=True, exist_ok=True)
    STATUS_DIR.mkdir(parents=True, exist_ok=True)
    messages = deque(maxlen=12)
    started_at = utc_now()
    status = {
        "schemaVersion": 1,
        "available": True,
        "appId": int(APP_ID),
        "phase": "preparing",
        "message": "Подготовка SteamCMD и общего хранилища клиента.",
        "progressPercent": 0.0,
        "downloadedBytes": 0,
        "downloadTotalBytes": 0,
        "installedBytes": directory_size(INSTALL_DIR),
        "expectedInstallBytes": EXPECTED_INSTALL_BYTES,
        "speedBytesPerSecond": 0,
        "etaSeconds": None,
        "diskFreeBytes": shutil.disk_usage(INSTALL_DIR).free,
        "startedAt": started_at,
        "updatedAt": started_at,
        "completedAt": None,
        "buildId": build_id(),
        "error": None,
        "recentMessages": [],
    }
    atomic_write(status)

    credentials = load_credentials()
    status["authenticationMode"] = "saved_account" if credentials else "anonymous"
    status["message"] = (
        "Авторизация сохранённого Steam-аккаунта и подготовка загрузки."
        if credentials
        else "Анонимная авторизация Steam и подготовка загрузки."
    )
    atomic_write(status)
    run_script = create_run_script(credentials)
    command = ["/usr/bin/steamcmd", "+runscript", str(run_script)]
    process = subprocess.Popen(
        command,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        bufsize=0,
    )
    stream = process.stdout
    os.set_blocking(stream.fileno(), False)

    buffer = ""
    last_size_check = 0.0
    last_status_write = 0.0
    last_downloaded = 0
    last_downloaded_at = time.monotonic()
    latest_error = None

    def process_line(raw_line):
        nonlocal last_downloaded, last_downloaded_at, latest_error
        line = clean_line(raw_line)
        if not line:
            return

        if any(pattern.lower() in line.lower() for pattern in USEFUL_PATTERNS):
            append_log(line)
            messages.append(line)

        progress = PROGRESS_PATTERN.search(line)
        if progress:
            now_monotonic = time.monotonic()
            downloaded = int(progress.group("done"))
            total = int(progress.group("total"))
            elapsed = max(0.001, now_monotonic - last_downloaded_at)
            delta = downloaded - last_downloaded
            speed = int(delta / elapsed) if delta > 0 else status["speedBytesPerSecond"]
            status["phase"] = phase_from_line(line)
            status["message"] = line
            status["progressPercent"] = max(0.0, min(100.0, float(progress.group("percent"))))
            status["downloadedBytes"] = downloaded
            status["downloadTotalBytes"] = total
            status["speedBytesPerSecond"] = max(0, speed)
            if speed > 0 and total > downloaded:
                status["etaSeconds"] = int((total - downloaded) / speed)
            else:
                status["etaSeconds"] = None
            last_downloaded = downloaded
            last_downloaded_at = now_monotonic

        lowered = line.lower()
        if "error!" in lowered or "failed" in lowered or "no subscription" in lowered:
            latest_error = line
        if "success!" in lowered and f"app '{APP_ID}'" in lowered:
            status["message"] = "Файлы Dota 2 загружены и проверены SteamCMD."

    while process.poll() is None:
        ready, _, _ = select.select([stream], [], [], 1.0)
        if ready:
            chunk = os.read(stream.fileno(), 16384)
            if chunk:
                buffer += chunk.decode("utf-8", errors="replace")
                parts = re.split(r"[\r\n]+", buffer)
                buffer = parts.pop()
                for part in parts:
                    process_line(part)

        now_monotonic = time.monotonic()
        if now_monotonic - last_size_check >= 10:
            status["installedBytes"] = directory_size(INSTALL_DIR)
            status["diskFreeBytes"] = shutil.disk_usage(INSTALL_DIR).free
            last_size_check = now_monotonic
        if now_monotonic - last_status_write >= 2:
            status["updatedAt"] = utc_now()
            status["recentMessages"] = list(messages)
            atomic_write(status)
            last_status_write = now_monotonic

    remaining = stream.read()
    if remaining:
        buffer += remaining.decode("utf-8", errors="replace")
    for part in re.split(r"[\r\n]+", buffer):
        process_line(part)

    return_code = process.wait()
    run_script.unlink(missing_ok=True)
    status["installedBytes"] = directory_size(INSTALL_DIR)
    status["diskFreeBytes"] = shutil.disk_usage(INSTALL_DIR).free
    status["updatedAt"] = utc_now()
    status["recentMessages"] = list(messages)
    status["buildId"] = build_id()

    executable = INSTALL_DIR / "game" / "dota.sh"
    if return_code == 0 and executable.exists():
        status["phase"] = "installed"
        status["message"] = "Клиент Dota 2 установлен и прошёл проверку файлов."
        status["progressPercent"] = 100.0
        status["completedAt"] = utc_now()
        status["error"] = None
    else:
        status["phase"] = "failed"
        status["message"] = "SteamCMD не смог завершить установку клиента."
        if return_code == 0 and not executable.exists():
            status["error"] = (
                "Steam не выдал игровые файлы аккаунту. Проверь лицензию Dota 2 "
                "и необходимость нового Steam Guard-кода."
                if credentials
                else "Анонимный SteamCMD не выдаёт игровые файлы Dota 2. "
                "Требуется сохранённый Steam-аккаунт."
            )
        else:
            status["error"] = latest_error or f"SteamCMD завершился с кодом {return_code}."

    atomic_write(status)

    while True:
        status["updatedAt"] = utc_now()
        status["diskFreeBytes"] = shutil.disk_usage(INSTALL_DIR).free
        atomic_write(status)
        time.sleep(30)


if __name__ == "__main__":
    main()
