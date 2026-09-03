"""JSONL stdin/stdout worker for the NeuraTrade TimesFM sidecar."""

from __future__ import annotations

import argparse
import json
import sys
from typing import Any

from .engine import EngineConfig, TimesFMEngine
from .protocol import ForecastRequest, ProtocolError, parse_request


def _request_id(payload: Any) -> str | None:
  if isinstance(payload, dict) and isinstance(payload.get("request_id"), str):
    return payload["request_id"]
  return None


def _write(payload: dict[str, Any]) -> None:
  sys.stdout.write(json.dumps(payload, separators=(",", ":"), allow_nan=False))
  sys.stdout.write("\n")
  sys.stdout.flush()


def _error_response(
  message: str, *, code: str, request_id: str | None = None
) -> dict[str, Any]:
  return {
    "ok": False,
    "request_id": request_id,
    "error": {"code": code, "message": message},
  }


def _parser() -> argparse.ArgumentParser:
  parser = argparse.ArgumentParser(description=__doc__)
  parser.add_argument(
    "--checkpoint", default="google/timesfm-3.0-pytorch", dest="checkpoint"
  )
  parser.add_argument("--device", default="auto")
  parser.add_argument("--batch-size", type=int, default=4)
  parser.add_argument("--cache-dir", default=None)
  parser.add_argument("--local-files-only", action="store_true")
  parser.add_argument("--torch-threads", type=int, default=0)
  parser.add_argument(
    "--validate-only",
    action="store_true",
    help="Validate JSONL requests without importing PyTorch or loading weights",
  )
  return parser


def _validated_response(request: ForecastRequest) -> dict[str, Any]:
  return {
    "ok": True,
    "request_id": request.request_id,
    "validated": True,
    "series_count": len(request.series),
    "horizon": request.horizon,
  }


def _forecast_response(engine: TimesFMEngine, request: ForecastRequest) -> dict[str, Any]:
  result = engine.forecast(request)
  return {
    "ok": True,
    "request_id": result.request_id,
    "latency_ms": result.latency_ms,
    "forecasts": [
      {
        "id": record.series_id,
        "target_names": list(record.target_names),
        "timestamps_ms": list(record.timestamps_ms),
        "forecast": record.forecast,
        "quantiles": record.quantiles,
      }
      for record in result.forecasts
    ],
  }


def main(argv: list[str] | None = None) -> int:
  args = _parser().parse_args(argv)
  engine: TimesFMEngine | None = None
  if not args.validate_only:
    try:
      engine = TimesFMEngine(
        EngineConfig(
          checkpoint_path=args.checkpoint,
          per_core_batch_size=args.batch_size,
          device=args.device,
          cache_dir=args.cache_dir,
          local_files_only=args.local_files_only,
          torch_threads=args.torch_threads,
        )
      )
    except Exception as error:  # model/import errors are reported to stderr only
      print(f"TimesFM worker initialization failed: {error}", file=sys.stderr)
      return 2

  for raw_line in sys.stdin:
    line = raw_line.strip()
    if not line:
      continue
    payload: Any = None
    request_id: str | None = None
    try:
      payload = json.loads(line)
      request_id = _request_id(payload)
      request = parse_request(payload)
      if args.validate_only:
        _write(_validated_response(request))
      else:
        assert engine is not None
        _write(_forecast_response(engine, request))
    except json.JSONDecodeError as error:
      _write(
        _error_response(
          f"invalid JSON: {error.msg}", code="invalid_json", request_id=request_id
        )
      )
    except ProtocolError as error:
      _write(
        _error_response(str(error), code=error.code, request_id=request_id)
      )
    except Exception as error:
      _write(
        _error_response(
          f"forecast failed: {error}", code="forecast_error", request_id=request_id
        )
      )

  return 0


if __name__ == "__main__":
  raise SystemExit(main())
