import pytest

from neuratrade_timesfm.protocol import ProtocolError, parse_request


def _request(**overrides):
  timestamps = [1_700_000_000_000 + index * 60_000 for index in range(32)]
  payload = {
    "request_id": "test-1",
    "horizon": 4,
    "interval_ms": 60_000,
    "series": [
      {
        "id": "BTC/USDT:USDT",
        "timestamps_ms": timestamps,
        "targets": [
          [100.0 + index for index in range(32)],
          [index / 10 for index in range(32)],
        ],
        "target_names": ["log_close", "log_volume"],
      }
    ],
  }
  payload.update(overrides)
  return payload


def test_parse_request_preserves_multivariate_shape_and_options():
  request = parse_request(
    {
      **_request(),
      "return_quantiles": False,
      "use_symmetric_averaging": False,
      "use_znorm": True,
    }
  )

  assert request.series[0].context_length == 32
  assert request.series[0].variate_count == 2
  assert request.series[0].target_names == ("log_close", "log_volume")
  assert request.return_quantiles is False
  assert request.use_symmetric_averaging is False
  assert request.use_znorm is True


def test_parse_request_preserves_past_and_past_future_covariates():
  payload = _request()
  payload["series"][0]["past_only_covariates"] = [
    [index / 100 for index in range(32)]
  ]
  payload["series"][0]["past_future_covariates"] = [
    [index / 100 for index in range(36)]
  ]

  request = parse_request(payload)

  assert request.series[0].past_only_covariates == (
    tuple(index / 100 for index in range(32)),
  )
  assert request.series[0].past_future_covariates == (
    tuple(index / 100 for index in range(36)),
  )


def test_parse_request_rejects_wrong_covariate_length():
  payload = _request()
  payload["series"][0]["past_only_covariates"] = [[1.0] * 31]

  with pytest.raises(ProtocolError, match="exactly 32 samples"):
    parse_request(payload)


def test_parse_request_rejects_nonregular_timestamps():
  payload = _request()
  payload["series"][0]["timestamps_ms"][10] += 1

  with pytest.raises(ProtocolError, match="strictly regular"):
    parse_request(payload)


def test_parse_request_rejects_nonfinite_targets():
  payload = _request()
  payload["series"][0]["targets"][0][4] = float("nan")

  with pytest.raises(ProtocolError, match="finite number"):
    parse_request(payload)
