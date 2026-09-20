#!/usr/bin/env python3
"""Run Go and Vite locally using the existing Compose database and Redis."""

import argparse
import fcntl
import http.client
import json
import os
from pathlib import Path
import shutil
import signal
import socket
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parent.parent
RUNTIME = ROOT / ".dev" / "source"
STATE = RUNTIME / "processes.json"


def inspect(name):
    result = subprocess.run(["docker", "inspect", name], capture_output=True, text=True)
    if result.returncode:
        raise RuntimeError(f"Cannot inspect {name}; start Docker and the existing Compose services first.")
    return json.loads(result.stdout)[0]


def endpoint(container, port):
    info = inspect(container)
    candidates = []
    for binding in (info["NetworkSettings"].get("Ports", {}).get(f"{port}/tcp") or []):
        host = binding["HostIp"]
        candidates.append(("127.0.0.1" if host in ("", "0.0.0.0", "::") else host, int(binding["HostPort"])))
    for network in info["NetworkSettings"]["Networks"].values():
        if network.get("IPAddress"):
            candidates.append((network["IPAddress"], port))
    candidates.append((container + ".orb.local", port))
    for host, mapped_port in candidates:
        try:
            with socket.create_connection((host, mapped_port), timeout=2):
                return host, mapped_port
        except OSError:
            pass
    raise RuntimeError(f"Cannot reach {container}:{port} from the host; publish its port on localhost or enable OrbStack networking.")


def load_state():
    return json.loads(STATE.read_text()) if STATE.exists() else {}


def save_state(state):
    staged = RUNTIME / "processes.next"
    staged.write_text(json.dumps(state, indent=2) + "\n")
    staged.chmod(0o600)
    os.replace(staged, STATE)


def running(process):
    if not process:
        return False
    result = subprocess.run(["ps", "-p", str(process["pid"]), "-o", "args="], capture_output=True, text=True)
    return result.returncode == 0 and process["marker"] in result.stdout


def stop_process(process):
    if not running(process):
        return
    pid = process["pid"]
    if os.getpgid(pid) != pid:
        raise RuntimeError("Source process group changed; refusing to signal an unrelated process.")
    os.killpg(pid, signal.SIGTERM)
    for _ in range(60):
        if not running(process):
            return
        time.sleep(0.25)
    raise RuntimeError("Source process is still shutting down; retry after checking its log.")


def stop(state):
    for name in ("frontend", "backend"):
        stop_process(state.get(name))
    save_state({})


def launch(command, env, name, marker):
    with (RUNTIME / f"{name}.log").open("ab") as output:
        process = subprocess.Popen(command, cwd=ROOT / ("backend" if name == "backend" else "frontend"),
                                   env=env, stdin=subprocess.DEVNULL, stdout=output, stderr=subprocess.STDOUT,
                                   start_new_session=True)
    return {"pid": process.pid, "marker": marker}


def ready(port, path, process, expected=None):
    for _ in range(90):
        if not running(process):
            break
        connection = http.client.HTTPConnection("127.0.0.1", port, timeout=1)
        try:
            connection.request("GET", path)
            response = connection.getresponse()
            body = response.read()
            if response.status == 200 and (expected is None or expected in body):
                return
        except OSError:
            pass
        finally:
            connection.close()
        time.sleep(1)
    raise RuntimeError(f"Source service on port {port} did not become ready; inspect logs in {RUNTIME}.")


def start(args, previous):
    app = inspect(args.app_container)
    env = os.environ.copy()
    env.update(dict(item.split("=", 1) for item in app["Config"]["Env"]
                    if item.split("=", 1)[0] not in {"PATH", "HOME", "HOSTNAME", "PWD", "SHLVL"}))
    data = next((Path(mount["Source"]) for mount in app["Mounts"]
                 if mount["Destination"] == "/app/data" and mount["Type"] == "bind"), None)
    if data is None or not (data / "config.yaml").is_file() or not (data / ".installed").exists():
        raise RuntimeError("An initialized /app/data bind mount is required; source startup reuses that configuration.")
    database_host, database_port = endpoint(args.postgres_container, 5432)
    redis_host, redis_port = endpoint(args.redis_container, 6379)
    for key, value in list(env.items()):
        if value.startswith("/app/data/"):
            env[key] = str(data / value[len("/app/data/"):])
        elif value.startswith("/app/resources/"):
            env[key] = str(ROOT / "backend/resources" / value[len("/app/resources/"):])
        if key.upper() in {"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"}:
            env[key] = value.replace("host.docker.internal", "127.0.0.1")
    no_proxy = ",".join(filter(None, [env.get("NO_PROXY"), "127.0.0.1,localhost,.orb.local", database_host, redis_host]))
    env.update(DATA_DIR=str(data), CONFIG_FILE=str(data / "config.yaml"), AUTO_SETUP="false",
               SERVER_HOST="127.0.0.1", SERVER_PORT=str(args.backend_port),
               DATABASE_HOST=database_host, DATABASE_PORT=str(database_port),
               REDIS_HOST=redis_host, REDIS_PORT=str(redis_port), NO_PROXY=no_proxy, no_proxy=no_proxy,
               LOG_OUTPUT_FILE_PATH=str(RUNTIME / "server.log"), GOTOOLCHAIN="auto")
    go, pnpm = shutil.which("go"), shutil.which("pnpm")
    if not go or not pnpm:
        raise RuntimeError("Go and pnpm must be installed and available on PATH.")
    if not (ROOT / "frontend/node_modules").exists():
        raise RuntimeError("Run pnpm --dir frontend install --frozen-lockfile first.")
    print("Building the native Go backend with the local build cache...", flush=True)
    commit = subprocess.check_output(["git", "rev-parse", "--short", "HEAD"], cwd=ROOT, text=True).strip()
    subprocess.run([go, "build", "-ldflags", f"-X main.Commit={commit}", "-o", str(RUNTIME / "server.next"), "./cmd/server"],
                   cwd=ROOT / "backend", env=env, check=True)
    stop(previous)
    os.replace(RUNTIME / "server.next", RUNTIME / "server")
    state = {"frontend_port": args.frontend_port, "backend_port": args.backend_port}
    was_running = app["State"]["Running"]
    try:
        state["backend"] = launch([str(RUNTIME / "server")], env, "backend", str(RUNTIME / "server"))
        save_state(state)
        ready(args.backend_port, "/health", state["backend"], b'"status":"ok"')
        if was_running:
            print("Stopping the Docker app; PostgreSQL and Redis continue running...", flush=True)
            subprocess.run(["docker", "stop", "--time", "30", args.app_container], check=True, stdout=subprocess.DEVNULL)
        frontend_env = os.environ.copy()
        frontend_env.setdefault("NODE_OPTIONS", "--max-old-space-size=4096")
        frontend_env.update(VITE_DEV_PROXY_TARGET=f"http://127.0.0.1:{args.backend_port}")
        command = [pnpm, "--dir", str(ROOT / "frontend"), "exec", "vite", "--config", str(ROOT / "frontend/vite.config.ts"), "--host", "127.0.0.1",
                   "--port", str(args.frontend_port), "--strictPort"]
        state["frontend"] = launch(command, frontend_env, "frontend", str(ROOT / "frontend"))
        save_state(state)
        ready(args.frontend_port, "/", state["frontend"], b"/src/main.ts")
        ready(args.frontend_port, "/health", state["frontend"], b'"status":"ok"')
    except Exception:
        stop(state)
        if was_running:
            subprocess.run(["docker", "start", args.app_container], check=False, stdout=subprocess.DEVNULL)
        raise
    show_status(state)


def show_status(state):
    for name in ("frontend", "backend"):
        print(f"{name}: {'running' if running(state.get(name)) else 'stopped'}")
    if state:
        print(f"Frontend: http://127.0.0.1:{state['frontend_port']}")
        print(f"Backend:  http://127.0.0.1:{state['backend_port']}")
    print(f"Logs: {RUNTIME}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["start", "restart", "stop", "status"])
    parser.add_argument("--app-container", default="sub2api-dev")
    parser.add_argument("--postgres-container", default="sub2api-postgres-dev")
    parser.add_argument("--redis-container", default="sub2api-redis-dev")
    parser.add_argument("--frontend-port", type=int, default=8080)
    parser.add_argument("--backend-port", type=int, default=8081)
    args = parser.parse_args()
    if not all(1 <= port <= 65535 for port in (args.frontend_port, args.backend_port)) or args.frontend_port == args.backend_port:
        parser.error("Frontend and backend need distinct ports between 1 and 65535.")
    RUNTIME.mkdir(parents=True, exist_ok=True, mode=0o700)
    lock = (RUNTIME / "launcher.lock").open("a")
    if args.action != "status":
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise RuntimeError("Another source startup or shutdown is in progress.") from None
    previous = load_state()
    if args.action == "status":
        show_status(previous)
    elif args.action == "stop":
        stop(previous)
        show_status({})
    elif args.action == "start" and all(running(previous.get(name)) for name in ("backend", "frontend")):
        show_status(previous)
    else:
        start(args, previous)


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, subprocess.CalledProcessError, OSError) as error:
        print(f"Source startup failed: {error}", file=sys.stderr)
        sys.exit(1)
