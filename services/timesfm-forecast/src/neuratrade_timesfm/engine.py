"""Lazy, persistent TimesFM 3 PyTorch inference wrapper."""

from __future__ import annotations

from dataclasses import dataclass
import time
from typing import Any

from .protocol import ForecastRequest, forecast_timestamps


@dataclass(frozen=True, slots=True)
class EngineConfig:
  """Runtime settings for one long-lived model process."""

  checkpoint_path: str = "google/timesfm-3.0-pytorch"
  per_core_batch_size: int = 4
  device: str = "auto"
  cache_dir: str | None = None
  local_files_only: bool = False
  torch_threads: int = 0


@dataclass(frozen=True, slots=True)
class ForecastRecord:
  """JSON-ready forecast for one input series."""

  series_id: str
  target_names: tuple[str, ...]
  timestamps_ms: tuple[int, ...]
  forecast: list[list[float]]
  quantiles: list[list[list[float]]] | None


@dataclass(frozen=True, slots=True)
class ForecastBatch:
  """JSON-ready result for one protocol request."""

  request_id: str
  latency_ms: float
  forecasts: tuple[ForecastRecord, ...]


def _json_array(value: Any, *, field: str) -> list[Any]:
  """Convert a NumPy result to JSON-compatible finite lists."""

  import numpy as np

  array = np.asarray(value)
  if not np.all(np.isfinite(array)):
    raise RuntimeError(f"TimesFM returned non-finite values in {field}")
  return array.tolist()


class TimesFMEngine:
  """Owns one loaded TimesFM model and serves batched requests."""

  def __init__(self, config: EngineConfig) -> None:
    import numpy as np
    import torch
    from timesfm3 import ModelConfig, TimesFM3Forecaster

    if config.per_core_batch_size < 1:
      raise ValueError("per_core_batch_size must be positive")
    if config.torch_threads < 0:
      raise ValueError("torch_threads cannot be negative")
    if config.torch_threads > 0:
      torch.set_num_threads(config.torch_threads)
      torch.set_num_interop_threads(max(1, min(config.torch_threads, 4)))

    device = None if config.device in ("", "auto") else config.device
    model_config = ModelConfig(
      checkpoint_path=config.checkpoint_path,
      per_core_batch_size=config.per_core_batch_size,
      device=device,
      cache_dir=config.cache_dir,
      local_files_only=config.local_files_only,
    )
    self._forecaster = TimesFM3Forecaster(config=model_config)
    self._numpy = np

  def forecast(self, request: ForecastRequest) -> ForecastBatch:
    """Run one validated batch through the already-loaded model."""

    contexts = [
      self._numpy.asarray(series.targets, dtype=self._numpy.float32)
      for series in request.series
    ]
    past_only_covariates = [
      (
        None
        if series.past_only_covariates is None
        else self._numpy.asarray(
          series.past_only_covariates, dtype=self._numpy.float32
        )
      )
      for series in request.series
    ]
    past_future_covariates = [
      (
        None
        if series.past_future_covariates is None
        else self._numpy.asarray(
          series.past_future_covariates, dtype=self._numpy.float32
        )
      )
      for series in request.series
    ]
    has_past_only_covariates = any(
      value is not None for value in past_only_covariates
    )
    has_past_future_covariates = any(
      value is not None for value in past_future_covariates
    )
    started = time.perf_counter()
    outputs = list(
      self._forecaster.predict_batch(
        contexts=contexts,
        horizon=request.horizon,
        past_only_covariates=(
          past_only_covariates if has_past_only_covariates else None
        ),
        past_future_covariates=(
          past_future_covariates if has_past_future_covariates else None
        ),
        ts_ids=[series.series_id for series in request.series],
        return_quantiles=request.return_quantiles,
        use_symmetric_averaging=request.use_symmetric_averaging,
        make_positive=False,
        sort_quantiles=True,
        use_znorm=request.use_znorm,
        padding_mode="none",
      )
    )
    elapsed_ms = (time.perf_counter() - started) * 1000
    if len(outputs) != len(request.series):
      raise RuntimeError(
        f"TimesFM returned {len(outputs)} outputs for {len(request.series)} series"
      )

    records: list[ForecastRecord] = []
    for series, output in zip(request.series, outputs):
      if output.forecast is None:
        raise RuntimeError(f"TimesFM returned no forecast for {series.series_id}")
      forecast = _json_array(output.forecast, field=f"{series.series_id}.forecast")
      quantiles = (
        _json_array(output.quantiles, field=f"{series.series_id}.quantiles")
        if output.quantiles is not None
        else None
      )
      records.append(
        ForecastRecord(
          series_id=series.series_id,
          target_names=series.target_names,
          timestamps_ms=tuple(forecast_timestamps(request, series)),
          forecast=forecast,
          quantiles=quantiles,
        )
      )

    return ForecastBatch(
      request_id=request.request_id,
      latency_ms=elapsed_ms,
      forecasts=tuple(records),
    )
