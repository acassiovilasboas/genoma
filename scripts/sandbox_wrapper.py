#!/usr/bin/env python3
"""
Genoma Sandbox Wrapper — Python
Executes a user script in the sandbox protocol format.
Communication: stdin (JSON) → script execution → stdout (JSON-lines)
"""
import sys
import json
import traceback
import importlib.util
import io
from contextlib import redirect_stdout, redirect_stderr


def emit(msg_type: str, data, tb: str = ""):
    """Write a protocol message to stdout."""
    msg = {"type": msg_type, "data": data}
    if tb:
        msg["traceback"] = tb
    print(json.dumps(msg), flush=True)


def main():
    try:
        # Read input from stdin
        raw_input = sys.stdin.read()
        if not raw_input.strip():
            emit("error", "No input received on stdin")
            sys.exit(1)

        payload = json.loads(raw_input)
        script_path = payload.get("script_path", "/workspace/script.py")
        user_input = payload.get("input", {})

        # Load the user script as a module
        spec = importlib.util.spec_from_file_location("user_script", script_path)
        if spec is None:
            emit("error", f"Cannot load script: {script_path}")
            sys.exit(1)

        module = importlib.util.module_from_spec(spec)

        # Capture stdout/stderr from user script
        captured_stdout = io.StringIO()
        captured_stderr = io.StringIO()

        # Inject input as a global variable
        module.__dict__["INPUT"] = user_input
        module.__dict__["emit_log"] = lambda msg: emit("log", str(msg))

        with redirect_stdout(captured_stdout), redirect_stderr(captured_stderr):
            spec.loader.exec_module(module)

        # Log captured output
        stdout_val = captured_stdout.getvalue()
        if stdout_val.strip():
            for line in stdout_val.strip().split("\n"):
                emit("log", line)

        stderr_val = captured_stderr.getvalue()
        if stderr_val.strip():
            for line in stderr_val.strip().split("\n"):
                emit("log", f"[stderr] {line}")

        # Get result from the module
        if hasattr(module, "RESULT"):
            result = module.RESULT
        elif hasattr(module, "main"):
            result = module.main(user_input)
        else:
            result = {"status": "completed"}

        # Ensure result is serializable
        if not isinstance(result, dict):
            result = {"value": result}

        emit("result", result)

    except json.JSONDecodeError as e:
        emit("error", f"Invalid JSON input: {e}")
        sys.exit(1)
    except Exception as e:
        emit("error", str(e), traceback.format_exc())
        sys.exit(1)


if __name__ == "__main__":
    main()
