"""Validated JSONL protocol shared by the TimesFM worker and Bun client.

The protocol deliberately carries timestamps and all target variates together.
TimesFM 3 can use native multivariate attention, but it does not validate market
bar cadence for callers. This module keeps malformed or look-ahead-prone input
outside the model boundary.
"""

from __future__ import annotations

from dataclasses import dataclass
import math
from typing import Any, Mapping


MIN_CONTEXT_LENGTH = 32
MAX_CONTEXT_LENGTH = 15_360
MAX_HORIZON = 1_024
MAX_SERIES = 64
MAX_TARGET_VARIATES = 32


class ProtocolError(ValueError):
  """Raised when a worker request violates the JSONL contract."""

  def __init__(self, message: str, *, code: str = "invalid_request") -> None:
    super().__init__(message)
    self.code = code


@dataclass(frozen=True, slots=True)
class ForecastSeries:
  """One regularly sampled multivariate context."""

  series_id: str
  timestamps_ms: tuple[int, ...]
  targets: tuple[tuple[float, ...], ...]
  target_names: tuple[str, ...]
  past_only_covariates: tuple[tuple[float, ...], ...] | None = None
  past_future_covariates: tuple[tuple[float, ...], ...] | None = None

  @property
  def context_length(self) -> int:
    return len(self.timestamps_ms)

  @property
  def variate_count(self) -> int:
    return len(self.targets)


@dataclass(frozen=True, slots=True)
class ForecastRequest:
  """A batch of forecasting requests for one worker round-trip."""

  request_id: str
  horizon: int
  interval_ms: int
  series: tuple[ForecastSeries, ...]
  return_quantiles: bool = True
  use_symmetric_averaging: bool = True
  use_znorm: bool = False


def _mapping(value: Any, *, field: str) -> Mapping[str, Any]:
  if not isinstance(value, Mapping):
    raise ProtocolError(f"{field} must be an object")
  return value


def _string(value: Any, *, field: str, max_length: int = 128) -> str:
  if not isinstance(value, str) or not value.strip():
    raise ProtocolError(f"{field} must be a non-empty string")
  result = value.strip()
  if len(result) > max_length:
    raise ProtocolError(f"{field} exceeds {max_length} characters")
  return result


def _integer(value: Any, *, field: str, minimum: int, maximum: int) -> int:
  if isinstance(value, bool) or not isinstance(value, int):
    raise ProtocolError(f"{field} must be an integer")
  if value < minimum or value > maximum:
    raise ProtocolError(f"{field} must be between {minimum} and {maximum}")
  return value


def _boolean(value: Any, *, field: str, default: bool) -> bool:
  if value is None:
    return default
  if not isinstance(value, bool):
    raise ProtocolError(f"{field} must be a boolean")
  return value


def _finite_number(value: Any, *, field: str) -> float:
  if isinstance(value, bool) or not isinstance(value, (int, float)):
    raise ProtocolError(f"{field} must be a finite number")
  result = float(value)
  if not math.isfinite(result):
    raise ProtocolError(f"{field} must be a finite number")
  return result


def _covariates(
  value: Any,
  *,
  field: str,
  expected_length: int,
) -> tuple[tuple[float, ...], ...]:
  if not isinstance(value, list) or not value:
    raise ProtocolError(f"{field} must be a non-empty array")
  if len(value) > MAX_TARGET_VARIATES:
    raise ProtocolError(
      f"{field} cannot contain more than {MAX_TARGET_VARIATES} variates"
    )
  covariates: list[tuple[float, ...]] = []
  for variate_index, raw_variate in enumerate(value):
    if not isinstance(raw_variate, list) or len(raw_variate) != expected_length:
      raise ProtocolError(
        f"{field}[{variate_index}] must contain exactly "
        f"{expected_length} samples"
      )
    covariates.append(
      tuple(
        _finite_number(
          sample,
          field=f"{field}[{variate_index}][{sample_index}]",
        )
        for sample_index, sample in enumerate(raw_variate)
      )
    )
  return tuple(covariates)


def parse_request(payload: Any) -> ForecastRequest:
  """Parse and validate one decoded JSON payload."""

  root = _mapping(payload, field="request")
  request_id = _string(root.get("request_id"), field="request_id")
  horizon = _integer(
    root.get("horizon"), field="horizon", minimum=1, maximum=MAX_HORIZON
  )
  interval_ms = _integer(
    root.get("interval_ms"), field="interval_ms", minimum=1, maximum=86_400_000
  )
  raw_series = root.get("series")
  if not isinstance(raw_series, list) or not raw_series:
    raise ProtocolError("series must be a non-empty array")
  if len(raw_series) > MAX_SERIES:
    raise ProtocolError(f"series cannot contain more than {MAX_SERIES} items")

  seen_ids: set[str] = set()
  series: list[ForecastSeries] = []
  for series_index, raw_item in enumerate(raw_series):
    item = _mapping(raw_item, field=f"series[{series_index}]")
    series_id = _string(item.get("id"), field=f"series[{series_index}].id")
    if series_id in seen_ids:
      raise ProtocolError(f"duplicate series id: {series_id}")
    seen_ids.add(series_id)

    raw_timestamps = item.get("timestamps_ms")
    if not isinstance(raw_timestamps, list):
      raise ProtocolError(f"series[{series_index}].timestamps_ms must be an array")
    context_length = len(raw_timestamps)
    if not MIN_CONTEXT_LENGTH <= context_length <= MAX_CONTEXT_LENGTH:
      raise ProtocolError(
        f"series[{series_index}] context must be between "
        f"{MIN_CONTEXT_LENGTH} and {MAX_CONTEXT_LENGTH} samples"
      )
    timestamps: list[int] = []
    for timestamp_index, raw_timestamp in enumerate(raw_timestamps):
      if isinstance(raw_timestamp, bool) or not isinstance(raw_timestamp, int):
        raise ProtocolError(
          f"series[{series_index}].timestamps_ms[{timestamp_index}] must be an integer"
        )
      timestamps.append(raw_timestamp)
    deltas = [
      right - left for left, right in zip(timestamps, timestamps[1:])
    ]
    if any(delta != interval_ms for delta in deltas):
      raise ProtocolError(
        f"series[{series_index}] timestamps must be strictly regular at "
        f"interval_ms={interval_ms}"
      )

    raw_targets = item.get("targets")
    if not isinstance(raw_targets, list) or not raw_targets:
      raise ProtocolError(f"series[{series_index}].targets must be a non-empty array")
    if len(raw_targets) > MAX_TARGET_VARIATES:
      raise ProtocolError(
        f"series[{series_index}] cannot contain more than "
        f"{MAX_TARGET_VARIATES} target variates"
      )
    targets: list[tuple[float, ...]] = []
    for variate_index, raw_variate in enumerate(raw_targets):
      if not isinstance(raw_variate, list) or len(raw_variate) != context_length:
        raise ProtocolError(
          f"series[{series_index}].targets[{variate_index}] must contain "
          f"exactly {context_length} samples"
        )
      targets.append(
        tuple(
          _finite_number(
            value,
            field=f"series[{series_index}].targets[{variate_index}][{sample_index}]",
          )
          for sample_index, value in enumerate(raw_variate)
        )
      )

    raw_target_names = item.get("target_names")
    if raw_target_names is None:
      target_names = tuple(f"target_{index}" for index in range(len(targets)))
    else:
      if not isinstance(raw_target_names, list) or len(raw_target_names) != len(
        targets
      ):
        raise ProtocolError(
          f"series[{series_index}].target_names must match target variates"
        )
      target_names = tuple(
        _string(
          name,
          field=f"series[{series_index}].target_names[{name_index}]",
          max_length=64,
        )
        for name_index, name in enumerate(raw_target_names)
      )

    raw_past_only_covariates = item.get("past_only_covariates")
    past_only_covariates = (
      None
      if raw_past_only_covariates is None
      else _covariates(
        raw_past_only_covariates,
        field=f"series[{series_index}].past_only_covariates",
        expected_length=context_length,
      )
    )
    raw_past_future_covariates = item.get("past_future_covariates")
    past_future_covariates = (
      None
      if raw_past_future_covariates is None
      else _covariates(
        raw_past_future_covariates,
        field=f"series[{series_index}].past_future_covariates",
        expected_length=context_length + horizon,
      )
    )

    series.append(
      ForecastSeries(
        series_id=series_id,
        timestamps_ms=tuple(timestamps),
        targets=tuple(targets),
        target_names=target_names,
        past_only_covariates=past_only_covariates,
        past_future_covariates=past_future_covariates,
      )
    )

  return ForecastRequest(
    request_id=request_id,
    horizon=horizon,
    interval_ms=interval_ms,
    series=tuple(series),
    return_quantiles=_boolean(
      root.get("return_quantiles"), field="return_quantiles", default=True
    ),
    use_symmetric_averaging=_boolean(
      root.get("use_symmetric_averaging"),
      field="use_symmetric_averaging",
      default=True,
    ),
    use_znorm=_boolean(root.get("use_znorm"), field="use_znorm", default=False),
  )


def forecast_timestamps(request: ForecastRequest, series: ForecastSeries) -> list[int]:
  """Return timestamps for the forecast horizon, strictly after the context."""

  last_timestamp = series.timestamps_ms[-1]
  return [
    last_timestamp + request.interval_ms * offset
    for offset in range(1, request.horizon + 1)
  ]
