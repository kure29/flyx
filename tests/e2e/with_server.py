#!/usr/bin/env python3
"""
Server lifecycle manager for E2E tests.
Manages both Go backend and Vite frontend servers.

Usage:
    python with_server.py --help
    python with_server.py -- pytest test_login.py -v
    python with_server.py --server "make run" --port 6365 --server "npm run dev" --port 3000 -- pytest -v
"""

import argparse
import json
import os
import signal
import socket
import subprocess
import sys
import time
from contextlib import contextmanager
from pathlib import Path
from typing import Optional


def find_free_port(start: int = 3000, max_tries: int = 100) -> int:
    """Find an available port starting from `start`."""
    for port in range(start, start + max_tries):
        try:
            with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
                s.bind(("", port))
                return port
        except OSError:
            continue
    raise RuntimeError(f"No free port found in range {start}-{start + max_tries}")


def wait_for_port(port: int, host: str = "localhost", timeout: float = 30.0) -> bool:
    """Wait for a port to become available."""
    start = time.time()
    while time.time() - start < timeout:
        try:
            with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
                s.settimeout(1)
                s.connect((host, port))
                return True
        except OSError:
            time.sleep(0.2)
    return False


class ServerProcess:
    """Manages a single server process."""

    def __init__(
        self,
        command: str,
        port: int,
        cwd: Optional[Path] = None,
        env: Optional[dict] = None,
        name: Optional[str] = None,
        ready_timeout: float = 30.0,
    ):
        self.command = command
        self.port = port
        self.cwd = cwd
        self.env = env or {}
        self.name = name or f"server-{port}"
        self.ready_timeout = ready_timeout
        self.process: Optional[subprocess.Popen] = None

    def start(self) -> bool:
        """Start the server process."""
        env = os.environ.copy()
        env.update(self.env)

        print(f"[{self.name}] Starting: {self.command}", file=sys.stderr)
        print(f"[{self.name}] Working directory: {self.cwd or '.'}", file=sys.stderr)
        print(f"[{self.name}] Expecting port: {self.port}", file=sys.stderr)

        self.process = subprocess.Popen(
            self.command,
            shell=True,
            cwd=self.cwd,
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            preexec_fn=os.setsid,
        )

        if wait_for_port(self.port, timeout=self.ready_timeout):
            print(f"[{self.name}] Ready on port {self.port}", file=sys.stderr)
            return True
        else:
            print(f"[{self.name}] Failed to start (timeout)", file=sys.stderr)
            self.stop()
            return False

    def stop(self):
        """Stop the server process."""
        if self.process:
            try:
                os.killpg(os.getpgid(self.process.pid), signal.SIGTERM)
                self.process.wait(timeout=5)
            except Exception:
                try:
                    os.killpg(os.getpgid(self.process.pid), signal.SIGKILL)
                except Exception:
                    pass
            self.process = None
            print(f"[{self.name}] Stopped", file=sys.stderr)

    def is_running(self) -> bool:
        """Check if the server is still running."""
        return self.process is not None and self.process.poll() is None


@contextmanager
def managed_servers(servers: list[ServerProcess]):
    """Context manager for multiple servers."""
    started = []
    try:
        for server in servers:
            if server.start():
                started.append(server)
            else:
                raise RuntimeError(f"Failed to start {server.name}")
        yield started
    finally:
        for server in reversed(started):
            server.stop()


def parse_args():
    parser = argparse.ArgumentParser(
        description="Server lifecycle manager for E2E tests",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  # Run all tests with default servers (backend + frontend)
  python with_server.py -- pytest -v

  # Run specific test file
  python with_server.py -- pytest test_login.py -v

  # Custom server configuration
  python with_server.py \\
    --server "make run" --port 6365 --cwd go-backend \\
    --server "pnpm run dev" --port 3000 --cwd vite-frontend \\
    -- pytest -v

  # Use custom backend port
  python with_server.py --backend-port 8080 -- pytest -v
""",
    )

    parser.add_argument(
        "--server",
        action="append",
        dest="servers",
        metavar="COMMAND",
        help="Server command to run (can be specified multiple times)",
    )
    parser.add_argument(
        "--port",
        action="append",
        dest="ports",
        type=int,
        metavar="PORT",
        help="Port for the corresponding --server (can be specified multiple times)",
    )
    parser.add_argument(
        "--cwd",
        action="append",
        dest="cwds",
        metavar="DIR",
        help="Working directory for the corresponding --server",
    )
    parser.add_argument(
        "--env",
        action="append",
        dest="envs",
        metavar="KEY=VALUE",
        help="Environment variable for the corresponding --server",
    )
    parser.add_argument(
        "--name",
        action="append",
        dest="names",
        metavar="NAME",
        help="Name for the corresponding --server (for logging)",
    )

    parser.add_argument(
        "--backend-port",
        type=int,
        default=6365,
        help="Port for backend server (default: 6365)",
    )
    parser.add_argument(
        "--frontend-port",
        type=int,
        default=3000,
        help="Port for frontend server (default: 3000)",
    )
    parser.add_argument(
        "--backend-cwd",
        default="go-backend",
        help="Working directory for backend (default: go-backend)",
    )
    parser.add_argument(
        "--frontend-cwd",
        default="vite-frontend",
        help="Working directory for frontend (default: vite-frontend)",
    )
    parser.add_argument(
        "--jwt-secret",
        default="test-secret-e2e-key-do-not-use-in-production",
        help="JWT secret for backend",
    )
    parser.add_argument(
        "--db-path",
        default=":memory:",
        help="Database path for backend (default: :memory: for SQLite in-memory)",
    )
    parser.add_argument(
        "--timeout",
        type=float,
        default=30.0,
        help="Timeout for server startup (default: 30s)",
    )

    parser.add_argument(
        "command",
        nargs=argparse.REMAINDER,
        help="Command to run after servers start (use -- to separate)",
    )

    return parser.parse_args()


def build_servers(args) -> list[ServerProcess]:
    """Build the list of servers to start."""
    servers = []
    root = Path(__file__).parent.parent.parent

    if args.servers:
        # Custom server configuration
        for i, cmd in enumerate(args.servers):
            port = (
                args.ports[i]
                if args.ports and i < len(args.ports)
                else find_free_port()
            )
            cwd = Path(args.cwds[i]) if args.cwds and i < len(args.cwds) else root
            if not cwd.is_absolute():
                cwd = root / cwd
            name = (
                args.names[i]
                if args.names and i < len(args.names)
                else f"server-{port}"
            )

            env = {}
            if args.envs:
                for j, e in enumerate(args.envs):
                    if "=" in e:
                        k, v = e.split("=", 1)
                        env[k] = v

            servers.append(
                ServerProcess(
                    command=cmd,
                    port=port,
                    cwd=cwd,
                    env=env,
                    name=name,
                    ready_timeout=args.timeout,
                )
            )
    else:
        # Default configuration: backend + frontend
        backend_env = {
            "SERVER_ADDR": f":{args.backend_port}",
            "JWT_SECRET": args.jwt_secret,
            "DB_PATH": args.db_path,
        }

        servers.append(
            ServerProcess(
                command="go run ./cmd/paneld",
                port=args.backend_port,
                cwd=root / args.backend_cwd,
                env=backend_env,
                name="backend",
                ready_timeout=args.timeout,
            )
        )

        frontend_env = {
            "VITE_API_BASE": f"http://localhost:{args.backend_port}",
        }

        servers.append(
            ServerProcess(
                command="pnpm run dev",
                port=args.frontend_port,
                cwd=root / args.frontend_cwd,
                env=frontend_env,
                name="frontend",
                ready_timeout=args.timeout,
            )
        )

    return servers


def main():
    args = parse_args()

    if not args.command:
        parser = argparse.ArgumentParser()
        parser.print_help()
        sys.exit(1)

    if args.command[0] == "--":
        args.command = args.command[1:]

    servers = build_servers(args)

    # Write server info to a temp file for tests to read
    server_info = {
        "backend_port": args.backend_port
        if not args.servers
        else servers[0].port
        if servers
        else 6365,
        "frontend_port": args.frontend_port
        if not args.servers
        else servers[1].port
        if len(servers) > 1
        else 3000,
        "jwt_secret": args.jwt_secret,
    }

    info_file = Path(__file__).parent / ".server_info.json"
    with open(info_file, "w") as f:
        json.dump(server_info, f)

    # Set environment variables for tests
    os.environ["E2E_BACKEND_PORT"] = str(server_info["backend_port"])
    os.environ["E2E_FRONTEND_PORT"] = str(server_info["frontend_port"])
    os.environ["E2E_JWT_SECRET"] = server_info["jwt_secret"]

    exit_code = 1
    try:
        with managed_servers(servers) as started:
            if not started:
                print("No servers started", file=sys.stderr)
                sys.exit(1)

            # Run the test command
            print(f"Running: {' '.join(args.command)}", file=sys.stderr)
            result = subprocess.run(args.command)
            exit_code = result.returncode
    except KeyboardInterrupt:
        print("\nInterrupted", file=sys.stderr)
        exit_code = 130
    except Exception as e:
        print(f"Error: {e}", file=sys.stderr)
        exit_code = 1
    finally:
        if info_file.exists():
            info_file.unlink()

    sys.exit(exit_code)


if __name__ == "__main__":
    main()
